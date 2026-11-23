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
	if !cache.Delete("name") {
		log.Fatal("name was not deleted")
	}
	if _, found = cache.Get("name"); found {
		log.Fatal("deleted name was still stored")
	}
}
