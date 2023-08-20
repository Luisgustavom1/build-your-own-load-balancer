package main

import (
	"fmt"
	"net/http"
)

func homeHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Hi, I'm answering")
	fmt.Fprintf(w, "Welcome to the home page!")
}

func main() {
	http.HandleFunc("/", homeHandler)

	http.ListenAndServe(":3000", nil)
}
