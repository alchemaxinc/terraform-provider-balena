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
				Config: fmt.Sprintf(`
provider "balena" {}

resource "balena_application" "test" {
  app_name        = "%s"
  device_type     = "raspberrypi4-64"
  organization_id = %s
  is_public       = true
}
`, appName, testAccOrgID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("balena_application.test", "app_name", appName),
					resource.TestCheckResourceAttr("balena_application.test", "is_public", "true"),
					resource.TestCheckResourceAttr("balena_application.test", "is_archived", "false"),
				),
			},
			{
				Config: fmt.Sprintf(`
provider "balena" {}

resource "balena_application" "test" {
  app_name        = "%s"
  device_type     = "raspberrypi4-64"
  organization_id = %s
  is_public       = true
}
`, updatedName, testAccOrgID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("balena_application.test", "app_name", updatedName),
					resource.TestCheckResourceAttr("balena_application.test", "is_public", "true"),
					resource.TestCheckResourceAttr("balena_application.test", "is_archived", "false"),
				),
			},
		},
	})
}

// testAccCheckOrganizationDestroy verifies that organizations created by a
// test have been deleted. Only applies to ad-hoc organizations — the shared
// test org is managed by TestMain and not subject to CheckDestroy.
func testAccCheckOrganizationDestroy(s *terraform.State) error {
	client := testAccNewClient()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "balena_organization" {
			continue
		}
		id, _ := parseID(rs.Primary.ID)
		_, err := client.GetOrganization(context.Background(), id)
		if err == nil {
			return fmt.Errorf("organization %s still exists", rs.Primary.ID)
		}
		if !balena.IsNotFound(err) {
			return fmt.Errorf("error checking organization %s: %s", rs.Primary.ID, err)
		}
	}
	return nil
}

// TestAccOrganization_basic exercises balena_organization directly and
// verifies that hyphenated handles (which were previously rejected by the
// handle validator regex) are accepted.
func TestAccOrganization_basic(t *testing.T) {
	testAccPreCheck(t)
	handle := fmt.Sprintf("tf-acc-org-%d", rand.Int63())
	name := fmt.Sprintf("TF Acc Org %s", handle)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckOrganizationDestroy,
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

// testAccCheckSSHKeyDestroy verifies that ssh keys created by a test have
// been deleted.
func testAccCheckSSHKeyDestroy(s *terraform.State) error {
	client := testAccNewClient()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "balena_ssh_key" {
			continue
		}
		id, _ := parseID(rs.Primary.ID)
		_, err := client.GetSSHKey(context.Background(), id)
		if err == nil {
			return fmt.Errorf("ssh key %s still exists", rs.Primary.ID)
		}
		if !balena.IsNotFound(err) {
			return fmt.Errorf("error checking ssh key %s: %s", rs.Primary.ID, err)
		}
	}
	return nil
}

// TestAccSSHKey_basic creates an SSH key, then re-applies the same config to
// exercise the Update path (which must be a no-op, not an error), and finally
// verifies ImportState.
func TestAccSSHKey_basic(t *testing.T) {
	testAccPreCheck(t)
	title := acctest.RandomWithPrefix("tf_acc_ssh")
	// Any valid SSH public key works for the Balena API; this is a throwaway
	// key with no matching private key.
	pubKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGj+J6N3rJLf0bW1Xs5PrkqRXq2y5+qGhKjZC1f5h0oT tf-acc-test@example.invalid"

	config := fmt.Sprintf(`
provider "balena" {}

resource "balena_ssh_key" "test" {
  title      = "%s"
  public_key = "%s"
}
`, title, pubKey)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckSSHKeyDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("balena_ssh_key.test", "title", title),
					resource.TestCheckResourceAttrSet("balena_ssh_key.test", "id"),
					resource.TestCheckResourceAttrSet("balena_ssh_key.test", "created_at"),
				),
			},
			{
				// Re-apply identical config. tf-plugin-testing runs a plan
				// here, so any spurious drift would trigger Update — which
				// must not return an error.
				Config:   config,
				PlanOnly: true,
			},
			{
				ResourceName:            "balena_ssh_key.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"public_key"},
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
