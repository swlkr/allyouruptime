package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	app, err := NewApp(log.Default())
	haltOn(err)
	fmt.Println("Server is listening on port 9001")
	http.ListenAndServe("localhost:9001", app)
}
