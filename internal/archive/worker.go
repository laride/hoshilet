package archive

import (
	"encoding/json"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"runtime/debug"
	"strconv"
)

func Worker(args []string) error {
	if len(args) != 5 {
		return fmt.Errorf("invalid worker arguments")
	}
	limit, e := strconv.ParseInt(args[2], 10, 64)
	if e != nil || limit < 1 {
		return fmt.Errorf("invalid size limit")
	}
	seconds, e := strconv.ParseUint(args[3], 10, 64)
	if e != nil || seconds == 0 {
		return fmt.Errorf("invalid CPU limit")
	}
	mb, e := strconv.ParseUint(args[4], 10, 64)
	if e != nil || mb < 256 {
		return fmt.Errorf("invalid memory limit")
	}
	debug.SetMemoryLimit(int64(mb<<20) / 2)
	for res, l := range map[int]unix.Rlimit{unix.RLIMIT_AS: {Cur: mb << 20, Max: mb << 20}, unix.RLIMIT_CPU: {Cur: seconds, Max: seconds}, unix.RLIMIT_NOFILE: {Cur: 64, Max: 64}, unix.RLIMIT_FSIZE: {Cur: uint64(limit), Max: uint64(limit)}} {
		if e = unix.Setrlimit(res, &l); e != nil {
			return e
		}
	}
	r, e := Extract(args[0], args[1], limit)
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(r)
}
