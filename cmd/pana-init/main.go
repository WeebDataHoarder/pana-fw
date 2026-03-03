//go:debug cryptocustomrand=1
package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/creack/pty"
	"github.com/gliderlabs/ssh"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ed25519"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/sys/unix"
)

// SftpHandler handler for SFTP subsystem
func SftpHandler(sess ssh.Session) {
	debugStream := io.Discard
	serverOptions := []sftp.ServerOption{
		sftp.WithDebug(debugStream),
	}
	server, err := sftp.NewServer(
		sess,
		serverOptions...,
	)
	if err != nil {
		return
	}
	defer server.Close()
	_ = server.Serve()
}

const defaultPassword = "panasonic"

const deterministicSeed = "panasonic"

const shell = "/mnt/usb/local/bin/bash"

const path_prepend = "/mnt/usb/local/bin"

type DeterministicRandom struct {
	state [32]byte
}

func NewDeterministicRandom() *DeterministicRandom {
	return &DeterministicRandom{
		state: sha256.Sum256([]byte(deterministicSeed)),
	}
}

func (r *DeterministicRandom) Read(p []byte) (n int, err error) {
	for i := range p {
		r.state = sha256.Sum256(r.state[:])
		p[i] = r.state[0]
	}
	return len(p), nil
}

func main() {
	str := os.Getenv("PATH")
	_ = os.Setenv("PATH", path_prepend+":"+str)

	_, privKey, err := ed25519.GenerateKey(NewDeterministicRandom())
	if err != nil {
		panic(err)
	}
	signer, err := gossh.NewSignerFromKey(privKey)
	if err != nil {
		panic(err)
	}

	server := ssh.Server{
		Addr: "0.0.0.0:2222",
		HostSigners: []ssh.Signer{
			signer,
		},
		SubsystemHandlers: map[string]ssh.SubsystemHandler{
			"sftp": SftpHandler,
		},
		Handler: func(s ssh.Session) {
			cmd := exec.Command(shell)
			cmd.Env = os.Environ()
			ptyReq, winCh, isPty := s.Pty()
			if isPty {
				cmd.Env = append(cmd.Env, fmt.Sprintf("TERM=%s", ptyReq.Term))
				f, err := pty.Start(cmd)
				if err != nil {
					panic(err)
				}
				go func() {
					for win := range winCh {
						_ = unix.IoctlSetWinsize(int(f.Fd()), unix.TIOCSWINSZ, &unix.Winsize{
							Row:    uint16(win.Height),
							Col:    uint16(win.Width),
							Xpixel: 0,
							Ypixel: 0,
						})
					}
				}()
				go func() {
					io.Copy(f, s) // stdin
				}()
				io.Copy(s, f) // stdout
				cmd.Wait()
			} else {
				io.WriteString(s, "No PTY requested.\n")
				s.Exit(1)
			}
		},
		PasswordHandler: func(ctx ssh.Context, password string) bool {
			return subtle.ConstantTimeCompare([]byte(password), []byte(defaultPassword)) == 1
		},
	}
	_ = server.ListenAndServe()
}
