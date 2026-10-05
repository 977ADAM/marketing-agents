package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
)

// BuildDatabaseURL escapes credentials as URL userinfo, preserving spaces and +.
func BuildDatabaseURL(user, pass, host, port, database string) (string, error) {
	if user == "" || host == "" {
		return "", fmt.Errorf("database user and host are required")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("invalid database port")
	}
	u := url.URL{Scheme: "mysql", User: url.UserPassword(user, pass), Host: net.JoinHostPort(host, port), Path: "/" + database}
	return u.String(), nil
}
