//go:build !windows

package main

import "errors"

func windowsUILanguages() ([]string, error) { return nil, errors.New("not Windows") }
