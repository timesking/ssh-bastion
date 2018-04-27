package main

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

type ACLSSHConfigServer struct {
	SSHConfigServer
	AclExtraParam string
}

var Servers map[string]ACLSSHConfigServer
var AWSServerList map[string][]string

func GenerateServers() {
	// Servers
	Servers = make(map[string]ACLSSHConfigServer)
	AWSServerList = make(map[string][]string)
	for k, v := range config.Servers {
		Servers[k] = ACLSSHConfigServer{
			SSHConfigServer: v,
		}
	}

	for lstKey, lst := range config.AWSInstances {
		regions := []string{}
		if lst.Regions != nil && len(lst.Regions) > 0 {
			regions = append(regions, lst.Regions...)
		} else {
			fregions, err := fetchRegion()
			if err != nil {
				log.Printf("Error fetchRegion: %s", err)
			} else {
				regions = append(regions, fregions...)
			}
		}

		var machines []string
		for _, region := range regions {
			sess := session.Must(session.NewSession(&aws.Config{
				Region: aws.String(region),
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
			} else {
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
							server.ConnectPath = strings.Replace(server.ConnectPath, "privateip", *instance.PrivateIpAddress, -1)
							server.ConnectPath = strings.Replace(server.ConnectPath, "publicip", *instance.PublicIpAddress, -1)
							asss := ACLSSHConfigServer{
								SSHConfigServer: server,
								AclExtraParam:   lstKey,
							}
							log.Printf("aws instances: %v", asss)
							Servers[nt] = asss
							machines = append(machines, nt)
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

	}
}

func (acl *SSHConfigACL) GetServerChoices() []string {
	choices := []string{}
	choices = append(choices, acl.AllowedServers...)

	for _, v := range acl.AWSAllowedServers {
		if l, ok := AWSServerList[v]; ok {
			choices = append(choices, l...)
		}
	}

	return choices
}

func fetchRegion() ([]string, error) {
	awsSession := session.Must(session.NewSession(&aws.Config{Region: aws.String("us-west-2")}))

	svc := ec2.New(awsSession)
	awsRegions, err := svc.DescribeRegions(&ec2.DescribeRegionsInput{})
	if err != nil {
		fmt.Println("22")
		return nil, err
	}

	regions := make([]string, 0, len(awsRegions.Regions))
	for _, region := range awsRegions.Regions {
		regions = append(regions, *region.RegionName)
	}

	return regions, nil
}
