package routes

import (
	"fmt"
	"net/http"
)

func Root(res http.ResponseWriter, req *http.Request) {
	fmt.Println("root")
}
