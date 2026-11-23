package main

import (
	"fmt"
	"log"

	"forgecache"
)

func main() {
	cache := forgecache.New()
	if err := cache.Set("name", []byte("Calyx"), 0); err != nil {
		log.Fatal(err)
	}
	value, found := cache.Get("name")
	if !found {
		log.Fatal("name was not stored")
	}
	fmt.Println(string(value))
	deleted, err := cache.Delete("name")
	if err != nil || !deleted {
		log.Fatal("name was not deleted")
	}
	if _, found = cache.Get("name"); found {
		log.Fatal("deleted name was still stored")
	}
}
