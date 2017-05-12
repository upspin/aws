package s3

import (
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"

	"upspin.io/cloud/storage"
	"upspin.io/log"
)

const (
	defaultTestBucketName = "upspin-test-scratch"
	defaultTestRegion     = "us-east-1"
)

var (
	client      storage.Storage
	testDataStr = fmt.Sprintf("This is test at %v", time.Now())
	testData    = []byte(testDataStr)
	fileName    = fmt.Sprintf("test-file-%d", time.Now().Second())

	testBucket = flag.String("test_bucket", defaultTestBucketName, "bucket name to use for testing")
	testRegion = flag.String("test_region", defaultTestRegion, "region to use for the test bucket")
	useAWS     = flag.Bool("use_aws", false, "enable to run aws tests; requires aws credentials")
)

// This is more of a regression test as it uses the running cloud
// storage in prod. However, since S3 is always available, we accept
// relying on it.
func TestPutAndDownload(t *testing.T) {
	err := client.Put(fileName, testData)
	if err != nil {
		t.Fatalf("Can't put: %v", err)
	}
	data, err := client.Download(fileName)
	if err != nil {
		t.Fatalf("Can't Download: %v", err)
	}
	if string(data) != testDataStr {
		t.Errorf("Expected %q got %q", testDataStr, string(data))
	}
}

func TestDelete(t *testing.T) {
	err := client.Put(fileName, testData)
	if err != nil {
		t.Fatal(err)
	}
	err = client.Delete(fileName)
	if err != nil {
		t.Fatalf("Expected no errors, got %v", err)
	}
	// Test the side effect after Delete.
	_, err = client.Download(fileName)
	if err == nil {
		t.Fatal("Expected an error, but got none")
	}
}

func TestMain(m *testing.M) {
	flag.Parse()
	if !*useAWS {
		log.Printf(`

cloud/storage/amazons3: skipping test as it requires S3 access. To enable this
test, ensure you are properly authorized to upload to an S3 bucket named by flag
-test_bucket and then set this test's flag -use_aws.

`)
		os.Exit(0)
	}

	// Create client that writes to test bucket.
	var err error
	client, err = storage.Dial("S3",
		storage.WithKeyValue("s3Region", *testRegion),
		storage.WithKeyValue("s3BucketName", *testBucket),
		storage.WithKeyValue("defaultACL", ACLPublicRead))
	if err != nil {
		log.Fatalf("cloud/storage/amazons3: couldn't set up client: %v", err)
	}
	if err := client.(*s3Impl).createBucket(); err != nil {
		log.Printf("cloud/storage/amazons3: createBucket failed: %v", err)
	}

	code := m.Run()

	// Clean up.
	if err := client.(*s3Impl).deleteBucket(); err != nil {
		log.Printf("cloud/storage/amazons3: deleteBucket failed: %v", err)
	}

	os.Exit(code)
}

func (s *s3Impl) createBucket() error {
	_, err := s.service.CreateBucket(&s3.CreateBucketInput{Bucket: aws.String(s.bucketName)})
	return err
}

func (s *s3Impl) deleteBucket() error {
	_, err := s.service.DeleteBucket(&s3.DeleteBucketInput{Bucket: aws.String(s.bucketName)})
	return err
}
