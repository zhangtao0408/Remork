package auth

import (
	"errors"
	"fmt"
	"net/http"
	"os"
)

var ErrUnauthorized = errors.New("unauthorized")

func TokenFromEnv(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	token, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("token env %q is not set", name)
	}
	return token, nil
}

func Authorize(r *http.Request, expected string) error {
	if expected == "" {
		return nil
	}
	if r.Header.Get("Authorization") != "Bearer "+expected {
		return ErrUnauthorized
	}
	return nil
}
