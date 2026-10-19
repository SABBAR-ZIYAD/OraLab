package main

import (
	"errors"
	"fmt"
	"golang.org/x/term"
	"os"
)

func readPasswordFromTerminal() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("a terminal is required; pipe the password with --password-stdin for automation")
	}
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(password), nil
}
