package main

import (
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

type ACLSSHConfigServer struct {
	SSHConfigServer
	AclExtraParam string
}

type refreshServerChan struct {
	Done chan bool
}

var serverFreshForce chan refreshServerChan
var serverRefreshOnce sync.Once
var serverRefreshLock sync.RWMutex
var Servers map[string]ACLSSHConfigServer
var AWSServerList map[string][]string
var allRegionsCache []string

func GenerateServers() error {
	serverRefreshLock.Lock()
	defer serverRefreshLock.Unlock()
	log.Println("GenerateServers refresh start")
	// Servers
	Servers = make(map[string]ACLSSHConfigServer)
	AWSServerList = make(map[string][]string)
	for k, v := range config.Servers {
		Servers[k] = ACLSSHConfigServer{
			SSHConfigServer: v,
		}
	}

	if allRegionsCache == nil || len(allRegionsCache) <= 0 {
		fregions, err := fetchRegion()
		if err != nil {
			log.Printf("Error fetchRegion: %s", err)
			// return err
		}
		allRegionsCache = fregions
	}

	for lstKey, lst := range config.AWSInstances {
		regions := []string{}
		if lst.Regions != nil && len(lst.Regions) > 0 {
			regions = append(regions, lst.Regions...)
		} else {
			regions = append(regions, allRegionsCache...)
		}
		var creds *credentials.Credentials
		creds = nil
		if len(lst.AwsAccessKey) > 0 || len(lst.AwsSecretKey) > 0 {
			//TODO: last "" should be token comes from assume role
			creds = credentials.NewStaticCredentials(lst.AwsAccessKey, lst.AwsSecretKey, "")
		}

		var machines []string
		for _, region := range regions {
			sess := session.Must(session.NewSession(&aws.Config{
				Region:      aws.String(region),
				Credentials: creds,
			}))

			ec2Svc := ec2.New(sess)
			params := &ec2.DescribeInstancesInput{
				Filters: []*ec2.Filter{
					&ec2.Filter{
						Name:   aws.String("instance-state-name"),
						Values: aws.StringSlice([]string{"running"}),
					},
				},
			}

			result, err := ec2Svc.DescribeInstances(params)
			if err != nil {
				fmt.Println("Error", err)
				return err
			}

			for _, reservation := range result.Reservations {
				for _, instance := range reservation.Instances {
					var nt string
					for _, t := range instance.Tags {
						if *t.Key == "Name" {
							nt = *t.Value
							break
						}
					}
					// machines = append(machines, fmt.Sprint(*instance.InstanceId, *instance.State.Name, *instance.PrivateIpAddress, nt))
					// fmt.Println(region, *instance.InstanceId, *instance.State.Name, *instance.PrivateIpAddress, nt)

					server := lst.SSHConfigServer
					if matched, err := regexp.MatchString(lst.RegexFilter, nt); matched {
						var privateip, publicip string
						if instance.PrivateIpAddress != nil {
							privateip = *instance.PrivateIpAddress
						} else {
							// even with protection of listing only running instance.
							// I got a nil pointer crash due to private ip is not ready yet.
							break
						}

						server.ConnectPath = strings.Replace(server.ConnectPath, "privateip", *instance.PrivateIpAddress, -1)

						if instance.PublicIpAddress != nil {
							publicip = *instance.PublicIpAddress
							//show public ip always, but change connect path only when publicip presents.
							if strings.Contains(server.ConnectPath, "publicip") {
								server.ConnectPath = strings.Replace(server.ConnectPath, "publicip", *instance.PublicIpAddress, -1)
							}
						} else {
							log.Printf("Instance %s, no public ip", *instance.InstanceId)
							continue
						}

						asss := ACLSSHConfigServer{
							SSHConfigServer: server,
							AclExtraParam:   lstKey,
						}
						// log.Printf("aws instances: %v", asss)
						// name := fmt.Sprintf("%s |***| %s |***| %s", region, nt, *instance.InstanceId)
						name := fmt.Sprintf("%s |***| %s |***| privateip:%s, publicip:%s", region, nt, privateip, publicip)
						Servers[name] = asss
						machines = append(machines, name)
						AWSServerList[lstKey] = machines
					} else {
						if err != nil {
							log.Printf("Error aws regex problem: %s", err)
						}
					}
				}
			}
		}
	}
	return nil
}

func (acl *SSHConfigACL) GetServerChoices() []string {
	serverRefreshLock.RLock()
	defer serverRefreshLock.RUnlock()

	choices := []string{}
	choices = append(choices, acl.AllowedServers...)

	for _, v := range acl.AWSAllowedServers {
		if l, ok := AWSServerList[v]; ok {
			sort.Strings(l)
			choices = append(choices, l...)
		}
	}

	return choices
}

func GetServerByChoice(server string) (ACLSSHConfigServer, bool) {
	serverRefreshLock.RLock()
	defer serverRefreshLock.RUnlock()

	if s, ok := Servers[server]; ok {
		return s, ok
	} else {
		return ACLSSHConfigServer{}, ok
	}
}

func fetchRegion() ([]string, error) {
	awsSession := session.Must(session.NewSession(&aws.Config{Region: aws.String("us-west-2")}))

	svc := ec2.New(awsSession)
	awsRegions, err := svc.DescribeRegions(&ec2.DescribeRegionsInput{})
	if err != nil {
		return nil, err
	}

	regions := make([]string, 0, len(awsRegions.Regions))
	for _, region := range awsRegions.Regions {
		regions = append(regions, *region.RegionName)
	}

	return regions, nil
}

func RefreshServers() {
	if err := GenerateServers(); err != nil {
		log.Fatalln("RefreshServers first time err: %s", err.Error())
	}

	serverRefreshOnce.Do(func() {
		serverFreshForce = make(chan refreshServerChan, 1)
		go func() {
			bias := time.Duration(1)
			for {
				select {
				case v := <-serverFreshForce:
					err := GenerateServers()
					if err != nil {
						bias = bias * 2
						if bias >= time.Duration(8) {
							bias = time.Duration(8)
						}
					} else {
						bias = time.Duration(1)
					}
					log.Println("serverFreshForce Done")
					v.Done <- true

				case <-time.After(2 * bias * time.Minute):
					serverFreshForce <- refreshServerChan{
						Done: make(chan bool, 1),
					}
				}
			}
		}()
	})
}
