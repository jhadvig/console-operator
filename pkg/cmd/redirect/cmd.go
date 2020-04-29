package redirect

import (
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

func NewConsoleRedirect() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "redirect",
		Short: "Start server for redirecting to Console",
		Run: func(command *cobra.Command, args []string) {
			redirectURL, _ := command.Flags().GetString("redirect-url")
			if redirectURL == "" {
				fmt.Println("error: no redirect URL provided")
				os.Exit(-1)
			}
			startRedirectServer(redirectURL)
		},
	}
	cmd.Flags().StringP("redirect-url", "r", "", "Set URL to which you want to be redirected")
	return cmd
}

func handleRedirect(res http.ResponseWriter, req *http.Request, redirectURL string) {
	fmt.Println("Got request for", req.URL.Path)
	http.Redirect(res, req, redirectURL+req.URL.Path, 301)
}

func startRedirectServer(redirectURL string) {
	port := 8080
	portstring := fmt.Sprintf(":%d", port)
	fmt.Println("Listening on port", port, "Redirecting to", redirectURL)
	redirectServer := http.NewServeMux()
	redirectServer.HandleFunc("/", func(res http.ResponseWriter, req *http.Request) {
		handleRedirect(res, req, redirectURL)
	})

	http.ListenAndServe(portstring, redirectServer)
}
