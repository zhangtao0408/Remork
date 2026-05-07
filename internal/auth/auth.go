package auth

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
)

var ErrUnauthorized = errors.New("unauthorized")

type TokenSource struct {
	Env  string
	File string
}

func TokenFromEnv(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	token, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("token env %q is not set", name)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", fmt.Errorf("token env %q is empty", name)
	}
	return token, nil
}

func TokenFromFile(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("token file %q cannot be read: %w", path, err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("token file %q is empty", path)
	}
	return token, nil
}

func TokenFromSource(src TokenSource) (string, error) {
	if strings.TrimSpace(src.Env) != "" {
		return TokenFromEnv(src.Env)
	}
	if strings.TrimSpace(src.File) != "" {
		return TokenFromFile(src.File)
	}
	return "", nil
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
