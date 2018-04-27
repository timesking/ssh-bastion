package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/terminal"
)

// type channelOpenDirectMsg struct {
// 	RAddr string
// 	RPort uint32
// 	LAddr string
// 	LPort uint32
// }

// // ChannelForward establishes a secure channel forward (ssh -W) to the server
// // requested by the user, assuming it is a permitted host.
// func (s *SSHServer) ChannelForward(session *Session, newChannel ssh.NewChannel) {
// 	var msg channelOpenDirectMsg
// 	ssh.Unmarshal(newChannel.ExtraData(), &msg)
// 	address := fmt.Sprintf("%s:%d", msg.RAddr, msg.RPort)

// 	permitted := false
// 	for _, remote := range session.Remotes {
// 		if remote == address {
// 			permitted = true
// 			break
// 		}
// 	}

// 	if !permitted {
// 		newChannel.Reject(ssh.Prohibited, "remote host access denied for user")
// 		return
// 	}

// 	// Log the selection
// 	if s.Selected != nil {
// 		if err := s.Selected(session, address); err != nil {
// 			newChannel.Reject(ssh.Prohibited, "access denied")
// 			return
// 		}
// 	}

// 	conn, err := net.Dial("tcp", address)
// 	if err != nil {
// 		newChannel.Reject(ssh.ConnectionFailed, fmt.Sprintf("error: %v", err))
// 		return
// 	}

// 	channel, reqs, err := newChannel.Accept()

// 	go ssh.DiscardRequests(reqs)
// 	var closer sync.Once
// 	closeFunc := func() {
// 		channel.Close()
// 		conn.Close()
// 	}

// 	go func() {
// 		io.Copy(channel, conn)
// 		closer.Do(closeFunc)
// 	}()

// 	go func() {
// 		io.Copy(conn, channel)
// 		closer.Do(closeFunc)
// 	}()
// }

type rw struct {
	io.Reader
	io.Writer
}

func (s *SSHServer) SessionForward(sshConn sshConnection, newChannel ssh.NewChannel, chans <-chan ssh.NewChannel) {
	rawsesschan, sessReqs, err := newChannel.Accept()
	if err != nil {
		log.Printf("Unable to Accept Session, closing connection...")
		sshConn.Close()
		return
	}

	startTime := time.Now()
	sesschan := NewLogChannel(startTime, rawsesschan, sshConn.User())

	// Proxy the channel and its requests
	var agentForwarding bool = false
	maskedReqs := make(chan *ssh.Request, 5)
	go func() {
		// For the pty-req and shell request types, we have to reply to those right away.
		// This is for PuTTy compatibility - if we don't, it won't allow any input.
		// We also have to change them to WantReply = false,
		// or a double reply will cause a fatal error client side.
		for req := range sessReqs {
			sesschan.LogRequest(req)
			if req.Type == "auth-agent-req@openssh.com" {
				agentForwarding = true
				if req.WantReply {
					req.Reply(true, []byte{})
				}
				continue
			} else if (req.Type == "pty-req") && (req.WantReply) {
				req.Reply(true, []byte{})
				req.WantReply = false
			} else if (req.Type == "shell") && (req.WantReply) {
				req.Reply(true, []byte{})
				req.WantReply = false
			}
			maskedReqs <- req
		}
	}()

	// Set the window header to SSH Relay login.
	fmt.Fprintf(sesschan, "%s]0;SSH Bastion Relay Login%s", []byte{27}, []byte{7})

	fmt.Fprintf(sesschan, "%s\r\n", GetMOTD())

	var remote SSHConfigServer
	var remote_name string

	if user, ok := config.Users[sshConn.User()]; !ok {
		fmt.Fprintf(sesschan, "User has no permitted remote hosts.\r\n")
		sesschan.Close()
		return
	} else {
		if acl, ok := config.ACLs[user.ACL]; !ok {
			fmt.Fprintf(sesschan, "Error processing server selection (Invalid ACL).\r\n")
			log.Printf("Invalid ACL detected for user %s.", sshConn.User())
			sesschan.Close()
			return
		} else {
			svr, err := InteractiveSelection(sesschan, "Please choose from the following servers:", acl)
			if err != nil {
				fmt.Fprintf(sesschan, "Error processing server selection.\r\n")
				sesschan.Close()
				return
			}

			if server, ok := GetServerByChoice(svr); !ok {
				fmt.Fprintf(sesschan, "Incorrectly Configured Server Selected.\r\n")
				sesschan.Close()
				return
			} else {
				remote_name = svr
				remote = server.SSHConfigServer
			}
		}
	}

	err = sesschan.SyncToFile(remote_name)
	if err != nil {
		fmt.Fprintf(sesschan, "Failed to Initialize Session.\r\n")
		sesschan.Close()
		return
	}

	WriteAuthLog("Connecting to remote for relay (%s) by %s from %s.", remote.ConnectPath, sshConn.User(), sshConn.RemoteAddr())
	fmt.Fprintf(sesschan, "Connecting to %s\r\n", remote_name)

	hostKeyCallback := ssh.InsecureIgnoreHostKey()
	if remote.HostPubKeyFiles != nil && len(remote.HostPubKeyFiles) > 0 {
		hostKeyCallback = func(hostname string, remote_addr net.Addr, key ssh.PublicKey) error {
			for _, keyFileName := range remote.HostPubKeyFiles {
				hostKeyData, err := ioutil.ReadFile(keyFileName)
				if err != nil {
					log.Printf("Error reading host key file (%s) for remote (%s): %s", keyFileName, remote_name, err)
					continue
				}

				hostKey, _, _, _, err := ssh.ParseAuthorizedKey(hostKeyData)
				if err != nil {
					log.Printf("Error parsing host key file (%s) for remote (%s): %s", keyFileName, remote_name, err)
					continue
				}

				if (key.Type() == hostKey.Type()) && (bytes.Compare(key.Marshal(), hostKey.Marshal()) == 0) {
					log.Printf("Accepting host public key from file (%s) for remote (%s).", keyFileName, remote_name)
					return nil
				}
			}
			WriteAuthLog("Host key validation failed for remote %s by user %s from %s.", remote.ConnectPath, sshConn.User(), remote_addr)
			return fmt.Errorf("HOST KEY VALIDATION FAILED - POSSIBLE MITM BETWEEN RELAY AND REMOTE")
		}
	}

	var clientConfig *ssh.ClientConfig
	clientConfig = &ssh.ClientConfig{
		User: sshConn.User(),
		Auth: []ssh.AuthMethod{
			ssh.PasswordCallback(func() (secret string, err error) {
				if secret, ok := sshConn.Permissions.Extensions["password"]; ok && config.Global.PassPassword {
					return secret, nil
				} else {
					//log.Printf("Prompting for password for remote...")
					t := terminal.NewTerminal(sesschan, "")
					s, err := t.ReadPassword(fmt.Sprintf("%s@%s password: ", clientConfig.User, remote_name))
					//log.Printf("Got password for remote auth, err: %s", err)
					return s, err
				}
			}),
		},
		HostKeyCallback: hostKeyCallback,
	}

	if len(remote.LoginUser) > 0 {
		clientConfig.User = remote.LoginUser
	}

	if len(remote.LoginPrivate) > 0 {
		hostKeyData, err := ioutil.ReadFile(remote.LoginPrivate)
		if err != nil {
			log.Printf("Error LoginPrivate reading host key file (%s) for remote (%s): %s", remote.LoginPrivate, remote_name, err)
			return
		}

		hostKey, err := ssh.ParsePrivateKey(hostKeyData)
		if err != nil {
			log.Printf("Error LoginPrivate parsing host key file (%s) for remote (%s): %s", remote.LoginPrivate, remote_name, err)
			return
		}

		clientConfig.Auth = append([]ssh.AuthMethod{ssh.PublicKeys(hostKey)}, clientConfig.Auth...)
	}

	// Set up the agent
	if agentForwarding {
		agentChan, agentReqs, err := sshConn.OpenChannel("auth-agent@openssh.com", nil)
		if err == nil {
			defer agentChan.Close()
			go ssh.DiscardRequests(agentReqs)

			// Set up the client
			ag := agent.NewClient(agentChan)

			// Make sure PK is first in the list if supported.
			clientConfig.Auth = append([]ssh.AuthMethod{ssh.PublicKeysCallback(ag.Signers)}, clientConfig.Auth...)
		}
	}

	log.Printf("Getting Ready to Dial Remote SSH %s", remote_name)
	client, err := ssh.Dial("tcp", remote.ConnectPath, clientConfig)
	if err != nil {
		fmt.Fprintf(sesschan, "Connect failed: %v\r\n", err)
		sesschan.Close()
		return
	}
	defer client.Close()
	log.Printf("Dialled Remote SSH Successfully...")

	// Forward the session channel
	log.Printf("Setting up channel to remote %s", remote_name)
	channel2, reqs2, err := client.OpenChannel("session", []byte{})
	if err != nil {
		fmt.Fprintf(sesschan, "Remote session setup failed: %v\r\n", err)
		sesschan.Close()
		return
	}
	WriteAuthLog("Connected to remote for relay (%s) by %s from %s.", remote.ConnectPath, sshConn.User(), sshConn.RemoteAddr())
	defer WriteAuthLog("Disconnected from remote for relay (%s) by %s from %s.", remote.ConnectPath, sshConn.User(), sshConn.RemoteAddr())

	log.Printf("Starting session proxy...")
	proxy(maskedReqs, reqs2, sesschan, channel2)

}

func proxy(reqs1, reqs2 <-chan *ssh.Request, channel1 *LogChannel, channel2 ssh.Channel) {
	var closer sync.Once
	closeFunc := func() {
		channel1.Close()
		channel2.Close()
	}

	defer closer.Do(closeFunc)

	closerChan := make(chan bool, 1)

	// From remote, to client.
	go func() {
		io.Copy(channel1, channel2)
		closerChan <- true
	}()

	go func() {
		io.Copy(channel2, channel1)
		closerChan <- true
	}()

	for {
		select {
		case req := <-reqs1:
			if req == nil {
				return
			}
			b, err := channel2.SendRequest(req.Type, req.WantReply, req.Payload)
			if err != nil {
				return
			}
			req.Reply(b, nil)
		case req := <-reqs2:
			if req == nil {
				return
			}
			b, err := channel1.SendRequest(req.Type, req.WantReply, req.Payload)
			if err != nil {
				return
			}
			req.Reply(b, nil)
		case <-closerChan:
			return
		}
	}
}
