// Command test lists the active contacts of an organisation, as a manual
// end-to-end check against the Fairgate API.
//
// It reads its configuration from the environment:
//
//	FAIRGATE_ORG_ID      organisation ID (required)
//	FAIRGATE_ACCESS_KEY  access key (required)
//	FAIRGATE_TEST        use the test endpoint if set
//	FAIRGATE_DEBUG       dump HTTP traffic, including credentials, to stderr if set
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"os/signal"

	"github.com/golang-jwt/jwt/v5"
	"thde.io/fairgate"
)

// fsaPublicKey verifies tokens issued by the Fairgate Standard API.
const fsaPublicKey = `-----BEGIN PUBLIC KEY-----
MIGbMBAGByqGSM49AgEGBSuBBAAjA4GGAAQA3K0kW2sFn6gt3wqKLuUv7LyuslJ/
6w5SJJIwtEBcfGJ0IbSRP9G3APZ3aTqy5fR66DsRSjLA6h5+4KwN5t4lr8cBhcDa
XXgEDjwHL8OZdC9Fa2V7i/UG5oCnIkCq55W4LBMtxqajBTOl0+sI/1MkYp0TPna9
HIzOjW9ndaDaZRXe5gw=
-----END PUBLIC KEY-----`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	orgID := os.Getenv("FAIRGATE_ORG_ID")
	accessKey := os.Getenv("FAIRGATE_ACCESS_KEY")
	if orgID == "" || accessKey == "" {
		return errors.New("FAIRGATE_ORG_ID and FAIRGATE_ACCESS_KEY must be set")
	}

	key, err := jwt.ParseECPublicKeyFromPEM([]byte(fsaPublicKey))
	if err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}

	opts := []fairgate.ClientOption{fairgate.WithAccessKey(accessKey)}
	if os.Getenv("FAIRGATE_TEST") != "" {
		opts = append(opts, fairgate.WithTest())
	}
	if os.Getenv("FAIRGATE_DEBUG") != "" {
		opts = append(opts, fairgate.WithHTTPClient(&http.Client{
			Transport: loggingTransport{next: http.DefaultTransport},
		}))
	}

	client := fairgate.New(orgID, key, opts...)

	for c, err := range client.ContactsIter(ctx) {
		if err != nil {
			return err
		}
		if c.Status != fairgate.ContactStatusActive {
			continue
		}

		fmt.Println(c.Basefields.FirstName, c.Basefields.LastName, c.Status)
	}

	return nil
}

// loggingTransport dumps requests and responses to stderr.
type loggingTransport struct {
	next http.RoundTripper
}

func (t loggingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if dump, err := httputil.DumpRequestOut(r, true); err == nil {
		fmt.Fprintf(os.Stderr, "%s\n\n", dump)
	}

	resp, err := t.next.RoundTrip(r)
	if err != nil {
		return nil, err
	}

	if dump, err := httputil.DumpResponse(resp, true); err == nil {
		fmt.Fprintf(os.Stderr, "%s\n\n", dump)
	}

	return resp, nil
}
