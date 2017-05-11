// Copyright 2017 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The upspin-setupstorage-aws command is an external upspin subcommand that
// executes the second step in establishing an upspinserver for AWS.
// Run upspin setupstorage-aws -help for more information.
package main // import "aws.upspin.io/cmd/upspin-setupstorage-aws"

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/s3"

	"upspin.io/subcmd"
)

type state struct {
	*subcmd.State
	sess *session.Session
}

const help = `
Setupstorage-aws is the second step in establishing an upspinserver.
It sets up AWS storage for your Upspin installation. You may skip this step
if you wish to store Upspin data on your server's local disk.
The first step is 'setupdomain' and the final step is 'setupserver'.

Setupstorage-aws creates an Amazon S3 bucket and an IAM role for accessing that
bucket. It then updates the server configuration files in $where/$domain/ to use
the specified bucket and region.

Before running this command, you should ensure you have an AWS account and that
the aws CLI command line tool works for you. For more information, visit
http://docs.aws.amazon.com/cli/latest/userguide/cli-chap-getting-set-up.html

If something goes wrong during the setup process, you can run the same command
with the -clean flag. It will attempt to remove any entities previously created
with the same options provided.
`

func main() {
	const name = "setupstorage-aws"

	log.SetFlags(0)
	log.SetPrefix("upspin setupstorage-aws: ")

	s := &state{
		State: subcmd.NewState(name),
	}
	var err error
	s.sess, err = session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	})
	if err != nil {
		s.Exitf("unable to create session: %s", err)
	}

	var (
		where    = flag.String("where", filepath.Join(os.Getenv("HOME"), "upspin", "deploy"), "`directory` to store private configuration files")
		domain   = flag.String("domain", "", "domain `name` for this Upspin installation")
		region   = flag.String("region", "us-east-1", "region for the S3 bucket")
		roleName = flag.String("role_name", "upspinstorage", "name for the IAM Role used to access the S3 bucket")
		clean    = flag.Bool("clean", false, "deletes all artifacts that would be created using this command")
	)

	s.ParseFlags(flag.CommandLine, os.Args[1:], help,
		"setupstorage-aws -domain=<name> [-region=<region>] [-clean] <bucket_name>")
	if flag.NArg() != 1 {
		s.Exitf("a single bucket name must be provided")
	}
	if len(*domain) == 0 {
		s.Exitf("the -domain flag must be provided")
	}

	bucketName := flag.Arg(0)
	if *clean {
		s.clean(*roleName, bucketName, *region)
		s.ExitNow()
	}

	cfgPath := filepath.Join(*where, *domain)
	cfg := s.ReadServerConfig(cfgPath)

	role, err := s.createRoleAccount(*roleName)
	if err != nil {
		s.Exitf("unable to create role account: %s", err)
	}

	if err := s.createInstanceProfile(role); err != nil {
		s.Exitf("unable to create instance profile: %s", err)
	}

	if err := s.createBucket(role, *region, bucketName); err != nil {
		s.Exitf("unable to create S3 bucket: %s", err)
	}

	if err := s.attachRolePolicy(role, bucketName); err != nil {
		s.Exitf("unable to attach role policy: %s", err)
	}

	cfg.StoreConfig = []string{
		"backend=S3",
		"defaultACL=public-read",
		"s3BucketName=" + bucketName,
		"s3Region=" + *region,
	}
	s.WriteServerConfig(cfgPath, cfg)

	fmt.Fprintf(os.Stderr, "You should now deploy the upspinserver binary and run 'upspin setupserver'.\n")
	s.ExitNow()
}

func (s *state) createRoleAccount(name string) (*iam.Role, error) {
	svc := iam.New(s.sess)
	rolePolicyDocument := aws.String(`{
		"Version": "2012-10-17",
	  "Statement": [
	    {
	      "Action": "sts:AssumeRole",
	      "Effect": "Allow",
	      "Principal": {
	          "Service": "ec2.amazonaws.com"
	      }
		  }
	  ]
	}`)
	output, err := svc.CreateRole(&iam.CreateRoleInput{
		RoleName:    aws.String(name),
		Description: aws.String("Used for storing data from the Upspin service"),

		AssumeRolePolicyDocument: rolePolicyDocument,
	})
	if err != nil {
		return nil, err
	}
	return output.Role, nil
}

func (s *state) createInstanceProfile(role *iam.Role) error {
	svc := iam.New(s.sess)
	if _, err := svc.CreateInstanceProfile(&iam.CreateInstanceProfileInput{
		InstanceProfileName: role.RoleName,
	}); err != nil {
		return err
	}

	_, err := svc.AddRoleToInstanceProfile(&iam.AddRoleToInstanceProfileInput{
		InstanceProfileName: role.RoleName,
		RoleName:            role.RoleName,
	})
	return err
}

func (s *state) attachRolePolicy(role *iam.Role, bucketName string) error {
	svc := iam.New(s.sess)
	_, err := svc.AttachRolePolicy(&iam.AttachRolePolicyInput{
		PolicyArn: aws.String("arn:aws:iam::aws:policy/AmazonS3FullAccess"),
		RoleName:  role.RoleName,
	})
	return err
}

func (s *state) createBucket(role *iam.Role, region, bucketName string) error {
	svc := s3.New(s.sess, &aws.Config{
		Region: aws.String(region),
	})

	_, err := svc.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})
	return err
}

// clean makes a best-effort attempt at cleaning up entities created by this
// command. Errors are reported to the user only if it wasn’t due to the entity
// not being found.
func (s *state) clean(roleName, bucketName, region string) {
	log.Println("Cleaning up...")

	s3svc := s3.New(s.sess, &aws.Config{
		Region: aws.String(region),
	})
	if _, err := s3svc.DeleteBucket(&s3.DeleteBucketInput{
		Bucket: aws.String(bucketName),
	}); err != nil {
		if err.(awserr.RequestFailure).StatusCode() != 404 {
			log.Printf("unable to delete bucket %s: %s", bucketName, err)
		}
	}

	iamSvc := iam.New(s.sess)
	if _, err := iamSvc.RemoveRoleFromInstanceProfile(&iam.RemoveRoleFromInstanceProfileInput{
		InstanceProfileName: aws.String(roleName),
		RoleName:            aws.String(roleName),
	}); err != nil {
		if err.(awserr.RequestFailure).StatusCode() != 404 {
			log.Printf("unable to remove role from instance profile %s: %s", roleName, err)
		}
	}
	if _, err := iamSvc.DeleteInstanceProfile(&iam.DeleteInstanceProfileInput{
		InstanceProfileName: aws.String(roleName),
	}); err != nil {
		if err.(awserr.RequestFailure).StatusCode() != 404 {
			log.Printf("unable to delete instance profile %s: %s", roleName, err)
		}
	}

	if _, err := iamSvc.DeleteRolePolicy(&iam.DeleteRolePolicyInput{
		PolicyName: aws.String("upspin-access-policy"),
		RoleName:   aws.String(roleName),
	}); err != nil {
		if err.(awserr.RequestFailure).StatusCode() != 404 {
			log.Printf("unable to delete role %s: %s", roleName, err)
		}
	}

	if _, err := iamSvc.DeleteRole(&iam.DeleteRoleInput{
		RoleName: aws.String(roleName),
	}); err != nil {
		if err.(awserr.RequestFailure).StatusCode() != 404 {
			log.Printf("unable to delete role %s: %s", roleName, err)
		}
	}
}
