package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
)

func main() {
	db, err := sql.Open("sqlite3", "allyouruptime.sqlite3")
	haltOn(err)
	model, err := NewModel(db)
	haltOn(err)
	//worker := NewWorker(log.Default(), model)
	//go worker.Work()
	app, err := NewApp(log.Default(), model)
	haltOn(err)
	fmt.Println("Server is listening on port 9001")
	http.ListenAndServe("localhost:9001", app)
}
