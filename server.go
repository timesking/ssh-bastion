package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/iam"
	"golang.org/x/crypto/ssh"
)

type SSHServer struct {
	sshConfig *ssh.ServerConfig
}

func NewSSHServer() (*SSHServer, error) {
	s := &SSHServer{
		sshConfig: &ssh.ServerConfig{
			NoClientAuth:  false,
			ServerVersion: "SSH-2.0-BASTION",
			AuthLogCallback: func(conn ssh.ConnMetadata, method string, err error) {
				if err != nil {
					WriteAuthLog("Failed %s for user %s from %s ssh2, %s", method, conn.User(), conn.RemoteAddr(), err.Error())
				} else {
					WriteAuthLog("Accepted %s for user %s from %s ssh2", method, conn.User(), conn.RemoteAddr())
				}
			},
			PasswordCallback: AuthUserPass,
			PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
				if user, ok := config.Users[conn.User()]; !ok {
					return nil, fmt.Errorf("User Not Found in Config for PK")
				} else {
					var authKeysDataAll []byte
					if len(user.AuthorizedKeysFile) > 0 {
						authKeysData, err := ioutil.ReadFile(user.AuthorizedKeysFile)
						if err != nil {
							log.Printf("Unable to read authorized keys file (%s) for user (%s): %s.", user.AuthorizedKeysFile, conn.User(), err)
							return nil, fmt.Errorf("Unable to read Authorized Keys file.")
						}
						authKeysDataAll = append(authKeysDataAll, authKeysData...)
					}

					if len(user.AwsUser) > 0 {
						var awsAuthKeys []string
						sess, err := session.NewSession(&aws.Config{
							Region: aws.String("us-west-2")},
						)
						if err != nil {
							log.Printf("AWS Session created failed. %s", err.Error())
						} else {
							// Create a IAM service client.
							svc := iam.New(sess)
							var listv iam.ListSSHPublicKeysInput
							listv.SetUserName(user.AwsUser)
							sshkeys, errssh := svc.ListSSHPublicKeys(&listv)
							if errssh != nil {
								log.Printf("Unable to read authorized keys for AWS User (%s) - (%s): %s.", conn.User(), user.AwsUser, errssh)
							} else {
								if sshkeys.SSHPublicKeys != nil {
									for _, sshKeyMeta := range sshkeys.SSHPublicKeys {
										sshi := &iam.GetSSHPublicKeyInput{}
										pubkey, err := svc.GetSSHPublicKey(
											sshi.SetEncoding("SSH").
												SetUserName(user.AwsUser).
												SetSSHPublicKeyId(*sshKeyMeta.SSHPublicKeyId))
										if err == nil {
											awsAuthKeys = append(awsAuthKeys, *pubkey.SSHPublicKey.SSHPublicKeyBody)
										} else {
											log.Printf("Unable to read authorized keys for AWS User (%s) - (%s): %s.", conn.User(), user.AwsUser, err)
										}
									}
								}
							}
						}

						if len(awsAuthKeys) > 0 {
							awsAuthKeysData := []byte(strings.Join(awsAuthKeys, "\n"))
							authKeysDataAll = append(authKeysDataAll, awsAuthKeysData...)
							authKeysDataAll = append(authKeysDataAll, []byte("\n")...)
						}
					}
					// log.Printf("All pub ssh rsa:  \n %s", string(authKeysDataAll))
					for {
						if len(authKeysDataAll) > 0 {
							var authKey ssh.PublicKey
							var err error
							authKey, _, _, authKeysDataAll, err = ssh.ParseAuthorizedKey(authKeysDataAll)
							if err != nil {
								log.Printf("Error while processing authorized keys file (%s) for user (%s), err (%v)", user.AuthorizedKeysFile, conn.User(), err)
								return nil, fmt.Errorf("Error while processing authorized keys file.")
							}

							if (key.Type() == authKey.Type()) && (bytes.Compare(key.Marshal(), authKey.Marshal()) == 0) {
								perm := &ssh.Permissions{
									Extensions: map[string]string{
										"authType": "pk",
									},
								}
								return perm, nil
							}
						} else {
							return nil, fmt.Errorf("No PKs Match - ACCESS DENIED")
						}
					}
					// } else {
					// 	return nil, fmt.Errorf("User has not authorized keys file specified.")
					// }
				}
			},
		},
	}

	for _, keyPath := range config.Global.HostKeyPaths {
		hostKey, err := ioutil.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("Unable to read host key file (%s): %s", keyPath, err)
		}

		signer, err := ssh.ParsePrivateKey(hostKey)
		if err != nil {
			return nil, fmt.Errorf("Invalid SSH Host Key (%s)", keyPath)
		}

		s.sshConfig.AddHostKey(signer)
	}

	return s, nil
}

func (s *SSHServer) ListenAndServe(addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	return s.Serve(l)
}

func (s *SSHServer) Serve(l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}

		go s.HandleConn(conn)
	}
}

type sshConnection struct {
	*ssh.ServerConn
}

func (sc *sshConnection) Close() {
	WriteAuthLog("Connection closed by %s (User: %s).", sc.RemoteAddr(), sc.User())
	sc.ServerConn.Close()
}

func (s *SSHServer) HandleConn(c net.Conn) {
	//log.Printf("Starting Accept SSH Connection...")

	sshConnRaw, chans, reqs, err := ssh.NewServerConn(c, s.sshConfig)
	if err != nil {
		//log.Printf("Exiting as there is a config problem...")
		c.Close()
		return
	}

	sshConn := sshConnection{sshConnRaw}
	WriteAuthLog("Connection Start by %s (User: %s).", sshConn.RemoteAddr(), sshConn.User())
	defer sshConn.Close()

	if sshConn.Permissions == nil || sshConn.Permissions.Extensions == nil {
		return
	}

	go ssh.DiscardRequests(reqs)

	var wg sync.WaitGroup
	for newChannel := range chans {
		if newChannel == nil {
			return
		}
		switch newChannel.ChannelType() {
		case "session":
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.SessionForward(sshConn, newChannel, chans)
			}()
		// case "direct-tcpip":
		// 	s.ChannelForward(session, newChannel)
		default:
			newChannel.Reject(ssh.UnknownChannelType, "connection flow not supported, only interactive sessions are permitted.")
		}
	}
	wg.Wait()
	//log.Printf("ALL OK, closing as nothing left to do...")

}
