//go:build integration

package provider

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	mathrand "math/rand"
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

// testAccOrgID holds the shared organization ID used by all tests. Created in TestMain unless BALENA_TEST_ORG_ID is
// set; the API does not let an API token delete it.
var testAccOrgID string

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"balena": providerserver.NewProtocol6WithError(New("test")()),
}

func specOf(table string) *TableSpec {
	for _, s := range GENERATED_SPECS {
		if s.Table == table {
			return s
		}
	}
	panic("no spec for " + table)
}

func TestMain(m *testing.M) {
	os.Exit(runIntegrationTests(m))
}

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
	client := testAccNewClient()
	org := specOf("organization")
	handle := fmt.Sprintf("tf_acc_%d", mathrand.Int63())
	var created balena.Row
	var err error
	for attempt := 1; attempt <= 5; attempt++ {
		created, err = client.Create(context.Background(), org.Root, org.Table, balena.Row{"name": "TF Acc Test " + handle, "handle": handle})
		var apiErr *balena.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.StatusCode != 429 {
			break
		}
		time.Sleep(time.Duration(attempt*30) * time.Second)
	}
	if err != nil {
		log.Fatalf("Failed to create shared test organization: %v", err)
	}
	id, err := balena.CreatedID(created)
	if err != nil {
		log.Fatal(err)
	}
	testAccOrgID = strconv.FormatInt(id, 10)
	log.Printf("Created shared test organization: ID=%s handle=%s", testAccOrgID, handle)
	defer func() {
		if delErr := client.Delete(context.Background(), org.Root, org.Table, id); delErr != nil {
			log.Printf("Warning: could not delete test organization %s: %v (manual cleanup may be required)", testAccOrgID, delErr)
		}
	}()
	return m.Run()
}

func testAccPreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("BALENA_API_TOKEN") == "" {
		t.Skip("BALENA_API_TOKEN must be set for acceptance tests")
	}
	if testAccOrgID == "" {
		t.Skip("shared test organization was not created")
	}
}

func testAccNewClient() *balena.Client {
	return balena.NewClient(os.Getenv("BALENA_API_URL"), os.Getenv("BALENA_API_TOKEN"), "test")
}

// testAccCheckDestroyed verifies that no row of the given Terraform type is left.
func testAccCheckDestroyed(typeName, table string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		spec := specOf(table)
		client := testAccNewClient()
		for _, rs := range s.RootModule().Resources {
			if rs.Type != typeName {
				continue
			}
			id, _ := strconv.ParseInt(rs.Primary.ID, 10, 64)
			_, err := client.Get(context.Background(), spec.Root, spec.Table, id, balena.Query{Select: []string{"id"}})
			if err == nil {
				return fmt.Errorf("%s %s still exists", typeName, rs.Primary.ID)
			}
			if !balena.IsNotFound(err) {
				return fmt.Errorf("checking %s %s: %w", typeName, rs.Primary.ID, err)
			}
		}
		return nil
	}
}

func testAccApplicationConfig(appName string) string {
	return fmt.Sprintf(`
provider "balena" {}

resource "balena_application" "test" {
  app_name        = %q
  device_type     = "raspberrypi4-64"
  organization_id = %s
}
`, appName, testAccOrgID)
}

func TestAccApplication_basic(t *testing.T) {
	testAccPreCheck(t)
	appName := acctest.RandomWithPrefix("tf_acc_app")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDestroyed("balena_application", "application"),
		Steps: []resource.TestStep{
			{
				Config: testAccApplicationConfig(appName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("balena_application.test", "app_name", appName),
					resource.TestCheckResourceAttrSet("balena_application.test", "id"),
					resource.TestCheckResourceAttrSet("balena_application.test", "slug"),
					resource.TestCheckResourceAttrSet("balena_application.test", "uuid"),
					resource.TestCheckResourceAttr("balena_application.test", "device_type", "raspberrypi4-64"),
					resource.TestCheckResourceAttr("balena_application.test", "class", "fleet"),
					resource.TestCheckResourceAttr("balena_application.test", "is_archived", "false"),
					resource.TestCheckResourceAttr("balena_application.test", "is_public", "false"),
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

// TestAccApplication_update renames a fleet: the update must PATCH only the changed field (sending is_archived=false
// to a fleet that is not archived is rejected by the API).
func TestAccApplication_update(t *testing.T) {
	testAccPreCheck(t)
	appName := acctest.RandomWithPrefix("tf_acc_app")
	updatedName := acctest.RandomWithPrefix("tf_acc_app_upd")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDestroyed("balena_application", "application"),
		Steps: []resource.TestStep{
			{Config: testAccApplicationConfig(appName)},
			{
				Config: testAccApplicationConfig(updatedName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("balena_application.test", "app_name", updatedName),
					resource.TestCheckResourceAttr("balena_application.test", "is_archived", "false"),
				),
			},
		},
	})
}

// TestAccOrganization_basic creates an organization through Terraform. The API does not let an API token delete
// organizations, so destroying it only removes it from the state and the organization leaks; set
// BALENA_SKIP_ORGANIZATION_TEST to skip it.
func TestAccOrganization_basic(t *testing.T) {
	testAccPreCheck(t)
	if os.Getenv("BALENA_SKIP_ORGANIZATION_TEST") != "" {
		t.Skip("BALENA_SKIP_ORGANIZATION_TEST is set")
	}
	handle := fmt.Sprintf("tf_acc_org_%d", mathrand.Int63())
	name := "TF Acc Org " + handle
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "balena" {}

resource "balena_organization" "test" {
  name   = %q
  handle = %q
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

func TestAccApplicationVariablesAndTags(t *testing.T) {
	testAccPreCheck(t)
	appName := acctest.RandomWithPrefix("tf_acc_env")
	config := func(value string) string {
		return testAccApplicationConfig(appName) + fmt.Sprintf(`
resource "balena_application_environment_variable" "test" {
  application_id = balena_application.test.id
  name           = "TEST_VAR"
  value          = %q
}

resource "balena_application_config_variable" "test" {
  application_id = balena_application.test.id
  name           = "RESIN_HOST_CONFIG_gpu_mem"
  value          = "128"
}

resource "balena_application_tag" "test" {
  application_id = balena_application.test.id
  tag_key        = "env"
  value          = %q
}

data "balena_application_environment_variable" "test" {
  application_id = balena_application.test.id
  name           = balena_application_environment_variable.test.name
}
`, value, value)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			testAccCheckDestroyed("balena_application", "application"),
			testAccCheckDestroyed("balena_application_environment_variable", "application_environment_variable"),
		),
		Steps: []resource.TestStep{
			{
				Config: config("first"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("balena_application_environment_variable.test", "value", "first"),
					resource.TestCheckResourceAttr("balena_application_config_variable.test", "name", "RESIN_HOST_CONFIG_gpu_mem"),
					resource.TestCheckResourceAttr("balena_application_tag.test", "value", "first"),
					resource.TestCheckResourceAttrPair("data.balena_application_environment_variable.test", "id", "balena_application_environment_variable.test", "id"),
				),
			},
			{
				Config: config("second"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("balena_application_environment_variable.test", "value", "second"),
					resource.TestCheckResourceAttr("balena_application_tag.test", "value", "second"),
				),
			},
			{
				ResourceName:      "balena_application_tag.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccDataSourceApplicationAndOrganization(t *testing.T) {
	testAccPreCheck(t)
	appName := acctest.RandomWithPrefix("tf_acc_ds")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDestroyed("balena_application", "application"),
		Steps: []resource.TestStep{{
			Config: testAccApplicationConfig(appName) + fmt.Sprintf(`
data "balena_application" "by_name" {
  app_name = balena_application.test.app_name
}

data "balena_application" "by_id" {
  id = balena_application.test.id
}

data "balena_organization" "test" {
  id = %s
}
`, testAccOrgID),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrPair("data.balena_application.by_name", "id", "balena_application.test", "id"),
				resource.TestCheckResourceAttr("data.balena_application.by_name", "device_type", "raspberrypi4-64"),
				resource.TestCheckResourceAttrPair("data.balena_application.by_id", "slug", "balena_application.test", "slug"),
				resource.TestCheckResourceAttr("data.balena_organization.test", "id", testAccOrgID),
				resource.TestCheckResourceAttrSet("data.balena_organization.test", "handle"),
			),
		}},
	})
}

// testAccPublicKey returns a fresh OpenSSH ed25519 public key.
func testAccPublicKey(t *testing.T) string {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	name := "ssh-ed25519"
	blob := []byte{0, 0, 0, byte(len(name))}
	blob = append(blob, name...)
	blob = append(blob, 0, 0, 0, byte(len(pub)))
	blob = append(blob, pub...)
	return name + " " + base64.StdEncoding.EncodeToString(blob)
}

// testAccUserID returns the user id of the token.
func testAccUserID(t *testing.T) int64 {
	data, err := testAccNewClient().Do(context.Background(), "GET", "/actor/v1/whoami", nil)
	if err != nil {
		t.Fatal(err)
	}
	var who struct {
		ActorType   string `json:"actorType"`
		ActorTypeID int64  `json:"actorTypeId"`
	}
	if err := json.Unmarshal(data, &who); err != nil || who.ActorType != "user" {
		t.Skipf("the token is not a user token (%v)", err)
	}
	return who.ActorTypeID
}

// TestAccUserPublicKey_basic registers an SSH key for the token's user.
func TestAccUserPublicKey_basic(t *testing.T) {
	testAccPreCheck(t)
	title := acctest.RandomWithPrefix("tf-acc-key")
	key := testAccPublicKey(t)
	user := testAccUserID(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDestroyed("balena_user_public_key", "user_public_key"),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "balena" {}

resource "balena_user_public_key" "test" {
  user_id    = %d
  title      = %q
  public_key = %q
}
`, user, title, key),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("balena_user_public_key.test", "title", title),
					resource.TestCheckResourceAttr("balena_user_public_key.test", "user_id", strconv.FormatInt(user, 10)),
				),
			},
			{
				ResourceName:      "balena_user_public_key.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccDeviceProfileOverride_basic needs a provisioned device running a release of a host OS application, which an
// API token cannot create, so it needs BALENA_TEST_DEVICE_ID, BALENA_TEST_HOST_APP_ID and BALENA_TEST_PROFILE_NAME.
func TestAccDeviceProfileOverride_basic(t *testing.T) {
	testAccPreCheck(t)
	deviceID := os.Getenv("BALENA_TEST_DEVICE_ID")
	hostAppID := os.Getenv("BALENA_TEST_HOST_APP_ID")
	profileName := os.Getenv("BALENA_TEST_PROFILE_NAME")
	if deviceID == "" || hostAppID == "" || profileName == "" {
		t.Skip("BALENA_TEST_DEVICE_ID, BALENA_TEST_HOST_APP_ID and BALENA_TEST_PROFILE_NAME must be set")
	}
	config := func(active string) string {
		return fmt.Sprintf(`
provider "balena" {}

resource "balena_device_profile_override" "test" {
  device_id           = %s
  profile_name        = %q
  host_application_id = %s
  %s
}
`, deviceID, profileName, hostAppID, active)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDestroyed("balena_device_profile_override", "device_profile_override"),
		Steps: []resource.TestStep{
			{
				Config: config("is_active = true"),
				Check:  resource.TestCheckResourceAttr("balena_device_profile_override.test", "is_active", "true"),
			},
			{
				Config: config("is_active = false"),
				Check:  resource.TestCheckResourceAttr("balena_device_profile_override.test", "is_active", "false"),
			},
			{
				ResourceName:      "balena_device_profile_override.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccDevice_basic adopts a device, renames, pins and unpins it. An API token cannot create a device, so the test
// needs BALENA_TEST_DEVICE_UUID and BALENA_TEST_RELEASE_COMMIT (a successful release of the device's fleet, ideally the
// one it already runs). It does not set os_release, so the device does not reboot.
func TestAccDevice_basic(t *testing.T) {
	testAccPreCheck(t)
	uuid := os.Getenv("BALENA_TEST_DEVICE_UUID")
	commit := os.Getenv("BALENA_TEST_RELEASE_COMMIT")
	if uuid == "" || commit == "" {
		t.Skip("BALENA_TEST_DEVICE_UUID and BALENA_TEST_RELEASE_COMMIT must be set")
	}
	testAccRestoreDeviceOnCleanup(t, uuid)
	deviceName := acctest.RandomWithPrefix("tf-acc-device")
	config := func(extra string) string {
		return fmt.Sprintf(`
provider "balena" {}

resource "balena_device" "test" {
  uuid        = %q
  device_name = %q
  %s
}
`, uuid, deviceName, extra)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(fmt.Sprintf("pinned_release = %q", commit)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("balena_device.test", "device_name", deviceName),
					resource.TestCheckResourceAttr("balena_device.test", "pinned_release", commit),
				),
			},
			{
				Config: config(""),
				Check:  resource.TestCheckNoResourceAttr("balena_device.test", "pinned_release"),
			},
			{
				ResourceName:      "balena_device.test",
				ImportState:       true,
				ImportStateId:     uuid,
				ImportStateVerify: true,
			},
		},
	})
}

// testAccRestoreDeviceOnCleanup restores the name and the pin of the device, because destroying balena_device leaves
// the device untouched.
func testAccRestoreDeviceOnCleanup(t *testing.T, uuid string) {
	ctx := context.Background()
	client := testAccNewClient()
	spec := specOf("device")
	rows, err := client.List(ctx, spec.Root, "device", balena.Query{
		Select: []string{"id", "device_name", "is_pinned_on__release"},
		Filter: balena.Eq("uuid", uuid),
		Top:    1,
	})
	if err != nil || len(rows) != 1 {
		t.Fatalf("reading device %s: %v", uuid, err)
	}
	id, _, _ := balena.ToInt64(rows[0]["id"])
	var pin any
	if p, ok, _ := balena.RefID(rows[0]["is_pinned_on__release"]); ok {
		pin = p
	}
	name := rows[0]["device_name"]
	t.Cleanup(func() {
		if err := client.Patch(ctx, spec.Root, "device", id, balena.Row{"device_name": name, "is_pinned_on__release": pin}); err != nil {
			t.Errorf("restoring device %s: %s", uuid, err)
		}
	})
}
