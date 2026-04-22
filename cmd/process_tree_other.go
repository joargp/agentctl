//go:build !linux

package cmd

func enableSubreaper() error {
	return nil
}
