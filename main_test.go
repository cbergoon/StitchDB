package stitchdb

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"
)

func setup() {
	c, _ := NewConfig(Persist, DirPath("stitch/test/db/"), Sync(NONE), ManageFrequency(1*time.Second), Developer, PerformanceMonitor, BucketFileMultLimit(10))
	s, _ := NewStitchDB(c)

	s.Open()

	opts, _ := NewBucketOptions(BTreeDegree(32), Geo)
	s.CreateBucket("test", opts)

	s.Update("test", func(t *Tx) error {
		t.CreateIndex("value", INT_INDEX)
		for i := 0; i < 256; i++ {
			eopt, _ := NewEntryOptions()
			e, _ := NewEntry("key-"+strconv.Itoa(i), "{ \"value\":\""+strconv.Itoa(256-i)+"\", \"coords\": ["+strconv.Itoa(i)+", "+strconv.Itoa(256-i)+"]}", true, eopt)
			t.Set(e)
		}
		return nil
	})

	//time.Sleep(time.Second * 2)

	s.Close()
}

func teardown() {
	err := os.RemoveAll("stitch/")
	if err != nil {
		fmt.Println(err)
	}
}

func TestMain(m *testing.M) {
	setup()
	retCode := m.Run()
	teardown()

	os.Exit(retCode)
}
