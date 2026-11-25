package caserule

import "golang.org/x/sys/unix"

// pcCaseSensitive is _PC_CASE_SENSITIVE of <sys/unistd.h>.
const pcCaseSensitive = 11

func of(dir string) (Rule, error) {
	v, err := unix.Pathconf(dir, pcCaseSensitive)
	switch {
	case err != nil:
		return Unknown, err
	case v == 1:
		return Sensitive, nil
	case v == 0:
		return Insensitive, nil
	}
	return Unknown, nil
}
