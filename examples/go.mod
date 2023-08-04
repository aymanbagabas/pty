module examples

go 1.20

replace github.com/creack/pty => ../

require (
	github.com/creack/pty v1.1.18
	golang.org/x/term v0.10.0
)

require golang.org/x/sys v0.10.0 // indirect
