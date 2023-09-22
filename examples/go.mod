module examples

go 1.20

replace github.com/creack/pty => ../

replace golang.org/x/sys => ../../../golang/sys

require (
	github.com/charmbracelet/ssh v0.0.0-20230822194956-1a051f898e09
	github.com/creack/pty v1.1.18
	github.com/u-root/u-root v0.11.0
	golang.org/x/crypto v0.0.0-20220826181053-bd7e27e6170d
	golang.org/x/term v0.12.0
)

require (
	github.com/anmitsu/go-shlex v0.0.0-20200514113438-38f4b401e2be // indirect
	golang.org/x/sys v0.12.0 // indirect
)
