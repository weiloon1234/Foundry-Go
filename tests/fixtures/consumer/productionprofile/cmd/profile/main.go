package main

import "foundry.test/consumer/productionprofile"

func main() {
	if err := productionprofile.Check(); err != nil {
		panic(err)
	}
}
