// Copyright 2017 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package s3 implements a storage backend that saves data to Amazon
// Simple Storage Service.
package s3 // import "aws.upspin.io/cloud/storage/s3"

import (
	"bytes"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"

	"upspin.io/cloud/storage"
	"upspin.io/errors"
)

// These constants define ACLs for writing data to Amazon Simple Storage
// Service. Definitions according to
// http://docs.aws.amazon.com/AmazonS3/latest/dev/acl-overview.html#canned-acl
const (
	// ACLPublicRead means owner gets FULL_CONTROL.
	// The AllUsers group gets READ access.
	ACLPublicRead = "public-read"
	// ACLPrivate means owner gets FULL_CONTROL.
	// No one else has access rights.
	ACLPrivate = "private"
)

// Keys used for storing dial options.
const (
	regionName  = "s3Region"
	bucketName  = "s3BucketName"
	defaultACL  = "defaultACL"
	endpointURL = "endpoint"
	pathstyle   = "pathstyle"
)

// s3Impl is an implementation of Storage that connects to an Amazon Simple
// Storage (S3) backend.
type s3Impl struct {
	service         *s3.S3
	bucketName      string
	defaultWriteACL string
}

// New initializes a Storage implementation that stores data to Amazon Simple
// Storage Service.
func New(opts *storage.Opts) (storage.Storage, error) {
	const op errors.Op = "cloud/storage/amazons3.New"

	region, ok := opts.Opts[regionName]
	if !ok {
		return nil, errors.E(op, errors.Invalid, errors.Errorf("%q option is required", regionName))
	}
	config := aws.Config{
		Region: aws.String(region),
	}
	if endpoint, ok := opts.Opts[endpointURL]; ok {
		config.Endpoint = aws.String(endpoint)
	}
	bucket, ok := opts.Opts[bucketName]
	if !ok {
		return nil, errors.E(op, errors.Invalid, errors.Errorf("%q option is required", bucketName))
	}
	acl, ok := opts.Opts[defaultACL]
	if !ok {
		return nil, errors.E(op, errors.Invalid, errors.Errorf("%q option is required", defaultACL))
	}
	shouldPathstyle, ok := opts.Opts[pathstyle]
	if !ok {
		return nil, errors.E(op, errors.Invalid, errors.Errorf("%q option is required", pathstyle))
	}
	if strings.TrimSpace(shouldPathstyle) == "true" {
		config.S3ForcePathStyle = aws.Bool(true)
	} else if strings.TrimSpace(shouldPathstyle) == "false" {
		config.S3ForcePathStyle = aws.Bool(false)
	} else {
		return nil, errors.E(op, errors.Invalid, errors.Errorf("%q must be true or false", pathstyle))
	}
	if acl != ACLPrivate && acl != ACLPublicRead {
		return nil, errors.E(op, errors.Invalid,
			errors.Errorf("valid ACL values for S3 are %s and %s", ACLPrivate, ACLPublicRead))
	}

	sess, err := session.NewSessionWithOptions(session.Options{
		Config:            config,
		SharedConfigState: session.SharedConfigEnable,
	})
	if err != nil {
		return nil, errors.E(op, errors.IO, errors.Errorf("unable to create Amazon session: %s", err))
	}

	return &s3Impl{
		service:         s3.New(sess),
		bucketName:      bucket,
		defaultWriteACL: acl,
	}, nil
}

func init() {
	storage.Register("S3", New)
}

// Guarantee we implement the Storage interface.
var _ storage.Storage = (*s3Impl)(nil)

// LinkBase implements Storage.
func (s *s3Impl) LinkBase() (base string, err error) {
	return s.service.Endpoint + "/" + s.bucketName + "/", nil
}

// Download implements Storage.
func (s *s3Impl) Download(ref string) ([]byte, error) {
	const op errors.Op = "cloud/storage/amazons3.Download"

	buf := aws.NewWriteAtBuffer([]byte{})
	d := s3manager.NewDownloaderWithClient(s.service)
	_, err := d.Download(buf, &s3.GetObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(ref),
	})
	if err != nil {
		if awsErr, ok := err.(awserr.RequestFailure); ok && awsErr.StatusCode() == 404 {
			return nil, errors.E(op, errors.NotExist, err)
		}
		return nil, errors.E(op, errors.IO, errors.Errorf(
			"unable to download ref %q from bucket %q: %s", ref, s.bucketName, err))
	}
	return buf.Bytes(), nil
}

// Put implements Storage.
func (s *s3Impl) Put(ref string, contents []byte) error {
	const op errors.Op = "cloud/storage/amazons3.Put"

	ul := s3manager.NewUploaderWithClient(s.service)
	_, err := ul.Upload(&s3manager.UploadInput{
		ACL:    aws.String(s.defaultWriteACL),
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(ref),
		Body:   bytes.NewBuffer(contents),
	})
	if err != nil {
		return errors.E(op, errors.IO, errors.Errorf(
			"unable to upload ref %q to bucket %q: %s", ref, s.bucketName, err))
	}
	return nil
}

// Delete implements Storage.
func (s *s3Impl) Delete(ref string) error {
	const op errors.Op = "cloud/storage/amazons3.Delete"

	_, err := s.service.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(ref),
	})
	if err != nil {
		return errors.E(op, errors.IO, errors.Errorf(
			"unable to delete ref %q from bucket %q: %s", ref, s.bucketName, err))
	}
	return nil
}

// Close implements Storage.
func (s *s3Impl) Close() {
	// Not much to do, the S3 service doesn’t require any cleanup.
	s.service = nil
	s.bucketName = ""
}
