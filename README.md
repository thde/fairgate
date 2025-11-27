# fairgate

[![Go Reference](https://pkg.go.dev/badge/thde.io/fairgate.svg)](https://pkg.go.dev/thde.io/fairgate) [![test](https://github.com/thde/fakeword/actions/workflows/test.yml/badge.svg)](https://github.com/thde/fakeword/actions/workflows/test.yml) [![Go Report Card](https://goreportcard.com/badge/thde.io/fairgate)](https://goreportcard.com/report/thde.io/fairgate)

This Go package wraps the [Fairgate Standard API](https://fsa.fairgate.ch/docs/fsa_openapi3) with a small, battery-included Go client.

## Features

- Built-in JWT handling with automatic refresh and rate-limit backoff.
- Strongly typed helpers for contacts, pagination metadata, and iterator-based traversal.

## Installation

```sh
go get thde.io/fairgate
```

## Getting Started

Import the module and construct a client with your organisation ID and Fairgate-issued ECDSA public key. The client defaults to the production endpoint; call `WithTest()` to target the sandbox.

### Creating a client

```go
package main

import (
	"context"
	"log"

	"github.com/golang-jwt/jwt/v5"
	"thde.io/fairgate"
)

func main() {
	key, err := jwt.ParseECPublicKeyFromPEM([]byte("PEM"))
	if err != nil {
		log.Fatalf("error parsing key: %v", err)
	}

	client := fairgate.New(
		"your-org-id",
		key,
		fairgate.WithUserAgent("my-app/1.0"),
	)
	if err = client.TokenCreate(context.TODO(), "access-key"); err != nil {
		log.Fatalf("error creating token: %v", err)
	}
}
```

### Managing tokens

Call `TokenCreate(ctx, accessKey)` yourself before invoking other endpoints. The client validates expiry, refreshes when needed, and surfaces `ErrNoAccessKey`, `ErrNoRefreshToken`, or `ErrStatus` for troubleshooting. Provide `WithAccessKey` if you want the client to lazily call `TokenCreate`.

### Streaming contacts with iterators

`ContactsIter` returns an `iter.Seq2` that walks through all pages while respecting pagination metadata:

```go
for contact, err := range client.ContactsIter(ctx) {
	if err != nil {
		log.Fatalf("iteration error: %v", err)
	}
	log.Println(contact)
}
```

## Error Handling and Retries

- HTTP responses outside the 2xx range return `ErrStatus` plus the HTTP status text.
- Rate limiting is handled with exponential-style waits using the `X-Ratelimit-Retry-After` header.
- API error payloads are surfaced through the typed `Response` wrapper, which aggregates messages and field errors.

## Testing

Run them with the standard toolchain:

```sh
go test ./...
```

## License

This project is distributed under the MIT License. See [`LICENSE`](LICENSE) for details.
