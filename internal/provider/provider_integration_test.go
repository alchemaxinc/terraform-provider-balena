//go:build integration

package provider

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// testAccOrgID holds the shared organization ID used by all tests.
// Created in TestMain, never deleted (Balena API doesn't support org deletion).
var testAccOrgID string

// testAccProtoV6ProviderFactories is used to instantiate the provider during
// acceptance testing. The factory function is invoked for every test step so
// the provider is always freshly configured.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"balena": providerserver.NewProtocol6WithError(New("test")()),
}

// TestMain sets up a shared organization for all integration tests.
// If BALENA_TEST_ORG_ID is set, that org is used directly (no creation).
// Otherwise, a new org is created and best-effort deleted after tests.
func TestMain(m *testing.M) {
	os.Exit(runIntegrationTests(m))
}

// runIntegrationTests encapsulates setup + teardown so that deferred cleanup
// runs even if a test panics. os.Exit is kept out of this function because it
// bypasses deferred calls.
func runIntegrationTests(m *testing.M) int {
	token := os.Getenv("BALENA_API_TOKEN")
	if token == "" {
		log.Println("BALENA_API_TOKEN not set, skipping integration tests")
		return 0
	}

	if id := os.Getenv("BALENA_TEST_ORG_ID"); id != "" {
		testAccOrgID = id
		log.Printf("Using pre-existing test organization: ID=%s", testAccOrgID)
		return m.Run()
	}

	client := balena.NewClient("", token, "test")
	handle := fmt.Sprintf("tf_acc_%d", rand.Int63())
	name := fmt.Sprintf("TF Acc Test %s", handle)

	var org *balena.Organization
	var err error
	for attempt := 1; attempt <= 5; attempt++ {
		log.Printf("Creating shared test organization (attempt %d/5): %s (%s)", attempt, name, handle)
		org, err = client.CreateOrganization(context.Background(), name, handle)
		if err == nil {
			break
		}
		var apiErr *balena.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 429 {
			wait := time.Duration(attempt*30) * time.Second
			log.Printf("Rate limited, waiting %s before retry...", wait)
			time.Sleep(wait)
			continue
		}
		log.Fatalf("Failed to create shared test organization: %v", err)
	}
	if err != nil {
		log.Fatalf("Failed to create shared test organization after retries: %v", err)
	}
	testAccOrgID = strconv.FormatInt(org.ID, 10)
	log.Printf("Created shared test organization: ID=%s handle=%s", testAccOrgID, handle)

	defer func() {
		log.Printf("Attempting to clean up shared test organization %s", testAccOrgID)
		if delErr := client.DeleteOrganization(context.Background(), org.ID); delErr != nil {
			log.Printf("Warning: could not delete test organization %s: %v (manual cleanup may be required)", testAccOrgID, delErr)
		} else {
			log.Printf("Deleted shared test organization %s", testAccOrgID)
		}
	}()

	return m.Run()
}

// testAccPreCheck validates that required environment variables are set.
func testAccPreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("BALENA_API_TOKEN") == "" {
		t.Skip("BALENA_API_TOKEN must be set for acceptance tests")
	}
	if testAccOrgID == "" {
		t.Skip("shared test organization was not created")
	}
}

// testAccNewClient creates a Balena API client for use in CheckDestroy functions.
func testAccNewClient() *balena.Client {
	return balena.NewClient("", os.Getenv("BALENA_API_TOKEN"), "test")
}

// testAccCheckApplicationDestroy verifies that the application has been deleted.
func testAccCheckApplicationDestroy(s *terraform.State) error {
	client := testAccNewClient()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "balena_application" {
			continue
		}
		id, _ := parseID(rs.Primary.ID)
		_, err := client.GetApplication(context.Background(), id)
		if err == nil {
			return fmt.Errorf("application %s still exists", rs.Primary.ID)
		}
		if !balena.IsNotFound(err) {
			return fmt.Errorf("error checking application %s: %s", rs.Primary.ID, err)
		}
	}
	return nil
}

// testAccOrgConfig returns the Terraform data source config to reference the
// shared test organization by ID.
func testAccOrgConfig() string {
	return fmt.Sprintf(`
data "balena_organization" "test" {
  id = %s
}
`, testAccOrgID)
}

func TestAccApplication_basic(t *testing.T) {
	testAccPreCheck(t)
	appName := acctest.RandomWithPrefix("tf_acc_app")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckApplicationDestroy,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "balena" {}

resource "balena_application" "test" {
  app_name        = "%s"
  device_type     = "raspberrypi4-64"
  organization_id = %s
}
`, appName, testAccOrgID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("balena_application.test", "app_name", appName),
					resource.TestCheckResourceAttrSet("balena_application.test", "id"),
					resource.TestCheckResourceAttrSet("balena_application.test", "slug"),
					resource.TestCheckResourceAttr("balena_application.test", "device_type", "raspberrypi4-64"),
				),
			},
			{
				ResourceName:      "balena_application.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccApplication_update(t *testing.T) {
	testAccPreCheck(t)
	appName := acctest.RandomWithPrefix("tf_acc_app")
	updatedName := acctest.RandomWithPrefix("tf_acc_app_upd")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckApplicationDestroy,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "balena" {}

resource "balena_application" "test" {
  app_name        = "%s"
  device_type     = "raspberrypi4-64"
  organization_id = %s
}
`, appName, testAccOrgID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("balena_application.test", "app_name", appName),
					resource.TestCheckResourceAttr("balena_application.test", "is_archived", "false"),
					resource.TestCheckResourceAttr("balena_application.test", "is_public", "false"),
				),
			},
			{
				// Rename the application. This exercises the Update path,
				// which must PATCH only changed fields — previously a bug
				// caused is_archived to be sent on every update, which the
				// API rejects for non-archived apps.
				Config: fmt.Sprintf(`
provider "balena" {}

resource "balena_application" "test" {
  app_name        = "%s"
  device_type     = "raspberrypi4-64"
  organization_id = %s
}
`, updatedName, testAccOrgID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("balena_application.test", "app_name", updatedName),
					resource.TestCheckResourceAttr("balena_application.test", "is_archived", "false"),
				),
			},
		},
	})
}

// TestAccOrganization_basic exercises balena_organization directly through
// the Terraform CLI, covering create / read / import. The shared test org
// used by other tests is created via the client, so this test is what
// actually exercises the resource's Framework lifecycle.
//
// CheckDestroy is intentionally omitted: the Balena API does not permit
// organization deletion with an API token, so the resource's Delete is a
// best-effort no-op that removes the org from state only. The test org will
// leak; manual cleanup may be required.
func TestAccOrganization_basic(t *testing.T) {
	testAccPreCheck(t)
	handle := fmt.Sprintf("tf_acc_org_%d", rand.Int63())
	name := fmt.Sprintf("TF Acc Org %s", handle)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "balena" {}

resource "balena_organization" "test" {
  name   = "%s"
  handle = "%s"
}
`, name, handle),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("balena_organization.test", "name", name),
					resource.TestCheckResourceAttr("balena_organization.test", "handle", handle),
					resource.TestCheckResourceAttrSet("balena_organization.test", "id"),
				),
			},
			{
				ResourceName:      "balena_organization.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccApplicationEnvVar_basic(t *testing.T) {
	testAccPreCheck(t)
	appName := acctest.RandomWithPrefix("tf_acc_env")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckApplicationDestroy,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "balena" {}

resource "balena_application" "test" {
  app_name        = "%s"
  device_type     = "raspberrypi4-64"
  organization_id = %s
}

resource "balena_application_env_var" "test" {
  application_id = balena_application.test.id
  name           = "TEST_VAR"
  value          = "test_value"
}

resource "balena_application_config_var" "test" {
  application_id = balena_application.test.id
  name           = "RESIN_HOST_CONFIG_gpu_mem"
  value          = "128"
}
`, appName, testAccOrgID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("balena_application_env_var.test", "name", "TEST_VAR"),
					resource.TestCheckResourceAttr("balena_application_env_var.test", "value", "test_value"),
					resource.TestCheckResourceAttr("balena_application_config_var.test", "name", "RESIN_HOST_CONFIG_gpu_mem"),
					resource.TestCheckResourceAttr("balena_application_config_var.test", "value", "128"),
				),
			},
		},
	})
}

func TestAccApplicationTag_basic(t *testing.T) {
	testAccPreCheck(t)
	appName := acctest.RandomWithPrefix("tf_acc_tag")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckApplicationDestroy,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "balena" {}

resource "balena_application" "test" {
  app_name        = "%s"
  device_type     = "raspberrypi4-64"
  organization_id = %s
}

resource "balena_application_tag" "test" {
  application_id = balena_application.test.id
  tag_key        = "env"
  value          = "test"
}
`, appName, testAccOrgID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("balena_application_tag.test", "tag_key", "env"),
					resource.TestCheckResourceAttr("balena_application_tag.test", "value", "test"),
				),
			},
		},
	})
}

func TestAccDataSourceApplication(t *testing.T) {
	testAccPreCheck(t)
	appName := acctest.RandomWithPrefix("tf_acc_ds")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckApplicationDestroy,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "balena" {}

resource "balena_application" "test" {
  app_name        = "%s"
  device_type     = "raspberrypi4-64"
  organization_id = %s
}

data "balena_application" "by_name" {
  app_name = balena_application.test.app_name
}

data "balena_application" "by_id" {
  id = balena_application.test.id
}
`, appName, testAccOrgID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.balena_application.by_name", "app_name", appName),
					resource.TestCheckResourceAttrPair("data.balena_application.by_name", "id", "balena_application.test", "id"),
					resource.TestCheckResourceAttrPair("data.balena_application.by_id", "app_name", "balena_application.test", "app_name"),
				),
			},
		},
	})
}
