// Command openapi prints the generated OpenAPI document (used to refresh packages/contracts).
package main

import (
	"encoding/json"
	"fmt"
	"os"

	httpapi "github.com/Prathameshppawar/ideavault/apps/api/internal/handler/http"
)

func main() {
	api := httpapi.New(httpapi.Options{})
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(api.OpenAPI()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
