package main

import (
	"errors"
	"time"

	"github.com/cbergoon/btree"
)

//Todo: Finish Tx Operations
//Todo: Implement Support for Indexes

type RbCtx struct {
	backward      map[string]*Entry
	forward       map[string]*Entry
	backwardIndex map[string]*Index
}

type Tx struct {
	db    *StitchDB
	bkt   *Bucket
	mode  RWMode
	rbctx *RbCtx
}

func NewTx(db *StitchDB, bkt *Bucket, mode RWMode) (*Tx, error) {
	return &Tx{
		db:   db,
		bkt:  bkt,
		mode: mode,
		rbctx: &RbCtx{
			//Holds the backward changes made during the transaction. Keys with a nil value were inserted
			//during the transaction and should be deleted. Keys with a non-nil value were deleted
			//during the transaction and should be inserted.
			backward: make(map[string]*Entry),
			//Holds the backward index changes made during the transaction. Keys with a nil value were created
			//and need to be deleted on rollback. Keys with a non-nil value were dropped and need to be replaced
			//with the value on rollback.
			backwardIndex: make(map[string]*Index),
			//Holds the forward changes made during the transaction. Keys with a nil value were deleted during
			//the transaction and should be deleted. Keys with a non-nil value were inserted during the transaction
			//and should be inserted.
			forward: make(map[string]*Entry),
		},
	}, nil
}

func (t *Tx) RollbackTx() error {
	for key, entry := range t.rbctx.backward {
		if entry == nil { //Entry was inserted during transaction; delete
			t.bkt.delete(&Entry{k: key})
			for _, ind := range t.bkt.indexes {
				ind.t.Delete(&Entry{k: key})
			}
		} else { //Entry was deleted or overwritten during transaction; insert
			t.bkt.insert(entry)
			for _, ind := range t.bkt.indexes {
				ind.t.ReplaceOrInsert(entry)
			}
		}
	}
	t.unlock()
	return nil
}

func (t *Tx) CommitTx() error {
	if !t.db.open {
		return errors.New("error: db is closed")
	}
	if t.mode == MODE_READ {
		return errors.New("error: cannot commit read only transaction")
	}
	if t.mode == MODE_READ_WRITE {
		for key, entry := range t.rbctx.forward {
			if entry == nil { //Entry was deleted or overwritten during transaction; delete/overwrite
				t.bkt.WriteDeleteEntry(&Entry{k: key})
			} else { //Entry was inserted during transaction; insert
				t.bkt.WriteInsertEntry(entry)
			}
		}
		t.bkt.WriteAOFBuf()
	}
	t.unlock()
	return nil
}

func (t *Tx) lock() {
	if t.mode == MODE_READ {
		t.bkt.bktlock.RLock()
	} else if t.mode == MODE_READ_WRITE {
		t.bkt.bktlock.Lock()
	}
}

func (t *Tx) unlock() {
	if t.mode == MODE_READ {
		t.bkt.bktlock.RUnlock()
	} else if t.mode == MODE_READ_WRITE {
		t.bkt.bktlock.Unlock()
	}
}

func (t *Tx) Ascend(f func(e *Entry) bool) error {
	i := func(i btree.Item) bool {
		eItem := i.(*Entry)
		return f(eItem)
	}
	t.bkt.data.Ascend(i)
	return nil
}

func (t *Tx) Descend(f func(e *Entry) bool) error {
	i := func(i btree.Item) bool {
		eItem := i.(*Entry)
		return f(eItem)
	}
	t.bkt.data.Descend(i)
	return nil
}

func (t *Tx) AscendIndex(index string, f func(e *Entry) bool) error {

	return nil
}

func (t *Tx) DescendIndex(index string, f func(e *Entry) bool) error {
	return nil
}

func (t *Tx) Get(e *Entry) (*Entry, error) {
	if !t.db.open || t.bkt == nil || !t.bkt.open {
		return nil, errors.New("error: cannot get entry; db is in invalid state")
	}
	res := t.bkt.get(e)
	if res != nil {
		if res.IsExpired() || res.IsInvalid() {
			return nil, nil
		}
	}
	return res, nil
}

func (t *Tx) Set(e *Entry) (*Entry, error) {
	if !t.db.open || t.bkt == nil || !t.bkt.open {
		return nil, errors.New("error: cannot set entry; db is in invalid state")
	}
	pres := t.bkt.insert(e)
	t.rbctx.backward[e.k] = pres
	t.rbctx.forward[e.k] = e
	return pres, nil
}

func (t *Tx) Delete(e *Entry) (*Entry, error) {
	if !t.db.open || t.bkt == nil || !t.bkt.open {
		return nil, errors.New("error: cannot delete entry; db is in invalid state")
	}
	dres := t.bkt.delete(e)
	if dres != nil {
		t.rbctx.backward[e.k] = dres
		t.rbctx.forward[e.k] = nil
	}
	return dres, nil
}

func (t *Tx) CreateIndex(pattern string) error {
	if !t.db.open || t.bkt == nil || !t.bkt.open {
		return errors.New("error: cannot create index; db is in invalid state")
	}
	curr, ok := t.bkt.indexes[pattern]
	if ok && curr != nil {
		return errors.New("error: cannot create index; index already exists")
	}
	//Create Index
	index, err := NewIndex(pattern, t.bkt)
	if err != nil {
		return errors.New("error: could not create index")
	}
	t.bkt.indexes[pattern] = index
	//Add to backward indexes with nil value
	t.rbctx.backwardIndex[pattern] = nil
	//Rebuild Index
	t.bkt.indexes[pattern].build(t.bkt)
	return nil
}

func (t *Tx) DropIndex(pattern string) error {
	if !t.db.open || t.bkt == nil || !t.bkt.open {
		return errors.New("error: cannot drop index; db is in invalid state")
	}
	//Add to backward indexes with pointer to index value
	index, ok := t.bkt.indexes[pattern]
	if !ok || index == nil {
		return errors.New("error: cannot drop; index does not exist")
	}
	t.rbctx.backwardIndex[pattern] = index
	//Set map pointer to nil, Delete entry from index map
	t.bkt.indexes[pattern] = nil
	delete(t.bkt.indexes, pattern)
	return nil
}

func (t *Tx) Indexes() ([]string, error) {
	var idxs []string
	for i := range t.bkt.indexes {
		idxs = append(idxs, i)
	}
	return idxs, nil
}

func (t *Tx) Min() (*Entry, error) {
	return nil, nil
}

func (t *Tx) Max() (*Entry, error) {
	return nil, nil
}

func (t *Tx) Has(e *Entry) (bool, error) {
	return false, nil
}

func (t *Tx) Degree() (int, error) {
	return 0, nil
}

func (t *Tx) ExpiresIn(key string) (time.Duration, error) {
	return time.Second, nil
}
