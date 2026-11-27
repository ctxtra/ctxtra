//go:build !darwin && !linux

package caserule

import "os"

func of(dir string) (Rule, error) {
	_, err := os.Stat(dir)
	return Unknown, err
}
