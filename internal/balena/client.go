// Package balena provides a Go client for the Balena Cloud REST API.
package balena

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DEFAULT_BASE_URL is the default Balena Cloud API endpoint.
const DEFAULT_BASE_URL = "https://api.balena-cloud.com"

// APP_DEVICE_TYPE_EXPAND is the OData $expand clause to resolve the device type slug on applications.
const APP_DEVICE_TYPE_EXPAND = "$expand=is_for__device_type($select=slug)"

// DEVICE_DEVICE_TYPE_EXPAND is the OData $expand clause to resolve the device type slug on devices.
const DEVICE_DEVICE_TYPE_EXPAND = "$expand=is_of__device_type($select=slug)"

// APIError represents an error response from the Balena API.
type APIError struct {
	StatusCode int
	Body       string
}

// Error implements the error interface.
func (e *APIError) Error() string {
	return fmt.Sprintf("API returned status %d: %s", e.StatusCode, e.Body)
}

// IsNotFound reports whether the error is a 404 Not Found response.
func IsNotFound(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}
	return false
}

// IsConflict reports whether the error is a 409 Conflict response.
func IsConflict(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusConflict
	}
	return false
}

// Client is the Balena Cloud REST API client.
type Client struct {
	baseURL    string
	apiToken   string
	userAgent  string
	httpClient *http.Client
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithHTTPClient sets a custom HTTP client for the Balena client.
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// NewClient creates a new Balena API client.
func NewClient(baseURL, apiToken, version string, opts ...ClientOption) *Client {
	if baseURL == "" {
		baseURL = DEFAULT_BASE_URL
	}
	c := &Client{
		baseURL:   baseURL,
		apiToken:  apiToken,
		userAgent: "terraform-provider-balena/" + version,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// pineResponse is the generic Pine.js OData response wrapper.
type pineResponse[T any] struct {
	D []T `json:"d"`
}

// ODataRef represents a Pine.js linked object with __id.
type ODataRef struct {
	ID int64 `json:"__id"`
}

// DeviceTypeRef represents an expanded device type navigation property.
type DeviceTypeRef struct {
	Slug string `json:"slug"`
}

// Application represents a Balena application resource.
type Application struct {
	ID         int64           `json:"id"`
	AppName    string          `json:"app_name"`
	Slug       string          `json:"slug"`
	DeviceType []DeviceTypeRef `json:"is_for__device_type"`
	Org        ODataRef        `json:"organization"`
	IsPublic   bool            `json:"is_public"`
	IsArchived bool            `json:"is_archived"`
}

// DeviceTypeSlug returns the resolved device type slug from the expanded navigation property.
func (a *Application) DeviceTypeSlug() string {
	if len(a.DeviceType) > 0 {
		return a.DeviceType[0].Slug
	}
	return ""
}

type applicationCreatePayload struct {
	AppName    string `json:"app_name"`
	DeviceType int64  `json:"is_for__device_type"`
	Org        int64  `json:"organization"`
	IsPublic   *bool  `json:"is_public,omitempty"`
}

// ListApplications returns all applications visible to the authenticated user.
func (c *Client) ListApplications(ctx context.Context) ([]Application, error) {
	return doList[Application](ctx, c, "/v6/application?"+APP_DEVICE_TYPE_EXPAND)
}

// GetApplication retrieves a single application by numeric ID.
func (c *Client) GetApplication(ctx context.Context, id int64) (*Application, error) {
	return doGetByID[Application](ctx, c, "/v6/application", id, APP_DEVICE_TYPE_EXPAND)
}

// GetApplicationByName retrieves a single application by its app_name.
func (c *Client) GetApplicationByName(ctx context.Context, name string) (*Application, error) {
	filter := "$filter=" + url.QueryEscape("app_name eq '"+escapeOData(name)+"'") + "&" + APP_DEVICE_TYPE_EXPAND
	items, err := doListFiltered[Application](ctx, c, "/v6/application", filter)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, &APIError{StatusCode: http.StatusNotFound, Body: fmt.Sprintf("application %q not found", name)}
	}
	return &items[0], nil
}

// applicationCreateResponse is the minimal shape returned by POST /v6/application
// (without $expand, nav properties come back as {"__id": N}).
type applicationCreateResponse struct {
	ID int64 `json:"id"`
}

// CreateApplication creates a new application in the given organization.
// The deviceType parameter is a slug (e.g. "raspberrypi5") which is resolved to a numeric ID.
func (c *Client) CreateApplication(ctx context.Context, appName, deviceType string, orgID int64, isPublic *bool) (*Application, error) {
	dt, err := c.GetDeviceTypeBySlug(ctx, deviceType)
	if err != nil {
		return nil, fmt.Errorf("resolving device type %q: %w", deviceType, err)
	}
	payload := applicationCreatePayload{
		AppName:    appName,
		DeviceType: dt.ID,
		Org:        orgID,
		IsPublic:   isPublic,
	}
	created, err := doCreate[applicationCreateResponse](ctx, c, "/v6/application", payload)
	if err != nil {
		return nil, err
	}
	return c.GetApplication(ctx, created.ID)
}

// UpdateApplication patches an existing application with the given fields.
func (c *Client) UpdateApplication(ctx context.Context, id int64, body map[string]interface{}) error {
	return doPatch(ctx, c, "/v6/application", id, body)
}

// DeleteApplication removes an application by ID.
func (c *Client) DeleteApplication(ctx context.Context, id int64) error {
	return doDelete(ctx, c, "/v6/application", id)
}

// DeviceType represents a Balena device type (e.g. raspberrypi5).
type DeviceType struct {
	ID   int64  `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// GetDeviceTypeBySlug retrieves a device type by its slug.
func (c *Client) GetDeviceTypeBySlug(ctx context.Context, slug string) (*DeviceType, error) {
	filter := "$filter=" + url.QueryEscape("slug eq '"+escapeOData(slug)+"'")
	items, err := doListFiltered[DeviceType](ctx, c, "/v6/device_type", filter)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, &APIError{StatusCode: http.StatusNotFound, Body: fmt.Sprintf("device type %q not found", slug)}
	}
	return &items[0], nil
}

// Device represents a Balena device resource.
type Device struct {
	ID                int64           `json:"id"`
	UUID              string          `json:"uuid"`
	DeviceName        string          `json:"device_name"`
	BelongsToApp      ODataRef        `json:"belongs_to__application"`
	DeviceType        []DeviceTypeRef `json:"is_of__device_type"`
	Status            string          `json:"status"`
	IsOnline          bool            `json:"is_online"`
	IPAddress         string          `json:"ip_address"`
	OSVersion         string          `json:"os_version"`
	SupervisorVersion string          `json:"supervisor_version"`
}

// DeviceTypeSlug returns the resolved device type slug from the expanded navigation property.
func (d *Device) DeviceTypeSlug() string {
	if len(d.DeviceType) > 0 {
		return d.DeviceType[0].Slug
	}
	return ""
}

// GetDeviceByUUID retrieves a single device by its UUID.
func (c *Client) GetDeviceByUUID(ctx context.Context, uuid string) (*Device, error) {
	filter := "$filter=" + url.QueryEscape("uuid eq '"+escapeOData(uuid)+"'") + "&" + DEVICE_DEVICE_TYPE_EXPAND
	items, err := doListFiltered[Device](ctx, c, "/v6/device", filter)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, &APIError{StatusCode: http.StatusNotFound, Body: fmt.Sprintf("device with uuid %q not found", uuid)}
	}
	return &items[0], nil
}

// ApplicationEnvVar represents an application-level environment variable.
type ApplicationEnvVar struct {
	ID    int64    `json:"id"`
	App   ODataRef `json:"application"`
	Name  string   `json:"name"`
	Value string   `json:"value"`
}

type appEnvVarCreatePayload struct {
	App   int64  `json:"application"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ListApplicationEnvVars returns all environment variables for an application.
func (c *Client) ListApplicationEnvVars(ctx context.Context, appID int64) ([]ApplicationEnvVar, error) {
	filter := fmt.Sprintf("$filter=application eq %d", appID)
	return doListFiltered[ApplicationEnvVar](ctx, c, "/v6/application_environment_variable", filter)
}

// GetApplicationEnvVar retrieves a single application environment variable by ID.
func (c *Client) GetApplicationEnvVar(ctx context.Context, id int64) (*ApplicationEnvVar, error) {
	return doGetByID[ApplicationEnvVar](ctx, c, "/v6/application_environment_variable", id)
}

// CreateApplicationEnvVar creates a new environment variable on an application.
func (c *Client) CreateApplicationEnvVar(ctx context.Context, appID int64, name, value string) (*ApplicationEnvVar, error) {
	payload := appEnvVarCreatePayload{App: appID, Name: name, Value: value}
	return doCreate[ApplicationEnvVar](ctx, c, "/v6/application_environment_variable", payload)
}

// UpdateApplicationEnvVar updates the value of an application environment variable.
func (c *Client) UpdateApplicationEnvVar(ctx context.Context, id int64, value string) error {
	return doPatch(ctx, c, "/v6/application_environment_variable", id, map[string]interface{}{"value": value})
}

// DeleteApplicationEnvVar removes an application environment variable by ID.
func (c *Client) DeleteApplicationEnvVar(ctx context.Context, id int64) error {
	return doDelete(ctx, c, "/v6/application_environment_variable", id)
}

// ApplicationConfigVar represents an application-level configuration variable.
type ApplicationConfigVar struct {
	ID    int64    `json:"id"`
	App   ODataRef `json:"application"`
	Name  string   `json:"name"`
	Value string   `json:"value"`
}

type appConfigVarCreatePayload struct {
	App   int64  `json:"application"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// GetApplicationConfigVar retrieves a single application config variable by ID.
func (c *Client) GetApplicationConfigVar(ctx context.Context, id int64) (*ApplicationConfigVar, error) {
	return doGetByID[ApplicationConfigVar](ctx, c, "/v6/application_config_variable", id)
}

// CreateApplicationConfigVar creates a new config variable on an application.
func (c *Client) CreateApplicationConfigVar(ctx context.Context, appID int64, name, value string) (*ApplicationConfigVar, error) {
	payload := appConfigVarCreatePayload{App: appID, Name: name, Value: value}
	return doCreate[ApplicationConfigVar](ctx, c, "/v6/application_config_variable", payload)
}

// UpdateApplicationConfigVar updates the value of an application config variable.
func (c *Client) UpdateApplicationConfigVar(ctx context.Context, id int64, value string) error {
	return doPatch(ctx, c, "/v6/application_config_variable", id, map[string]interface{}{"value": value})
}

// DeleteApplicationConfigVar removes an application config variable by ID.
func (c *Client) DeleteApplicationConfigVar(ctx context.Context, id int64) error {
	return doDelete(ctx, c, "/v6/application_config_variable", id)
}

// DeviceEnvVar represents a device-level environment variable.
type DeviceEnvVar struct {
	ID     int64    `json:"id"`
	Device ODataRef `json:"device"`
	Name   string   `json:"name"`
	Value  string   `json:"value"`
}

type deviceEnvVarCreatePayload struct {
	Device int64  `json:"device"`
	Name   string `json:"name"`
	Value  string `json:"value"`
}

// GetDeviceEnvVar retrieves a single device environment variable by ID.
func (c *Client) GetDeviceEnvVar(ctx context.Context, id int64) (*DeviceEnvVar, error) {
	return doGetByID[DeviceEnvVar](ctx, c, "/v6/device_environment_variable", id)
}

// CreateDeviceEnvVar creates a new environment variable on a device.
func (c *Client) CreateDeviceEnvVar(ctx context.Context, deviceID int64, name, value string) (*DeviceEnvVar, error) {
	payload := deviceEnvVarCreatePayload{Device: deviceID, Name: name, Value: value}
	return doCreate[DeviceEnvVar](ctx, c, "/v6/device_environment_variable", payload)
}

// UpdateDeviceEnvVar updates the value of a device environment variable.
func (c *Client) UpdateDeviceEnvVar(ctx context.Context, id int64, value string) error {
	return doPatch(ctx, c, "/v6/device_environment_variable", id, map[string]interface{}{"value": value})
}

// DeleteDeviceEnvVar removes a device environment variable by ID.
func (c *Client) DeleteDeviceEnvVar(ctx context.Context, id int64) error {
	return doDelete(ctx, c, "/v6/device_environment_variable", id)
}

// DeviceServiceEnvVar represents a per-service device environment variable.
type DeviceServiceEnvVar struct {
	ID             int64    `json:"id"`
	ServiceInstall ODataRef `json:"service_install"`
	Name           string   `json:"name"`
	Value          string   `json:"value"`
}

type deviceServiceEnvVarCreatePayload struct {
	ServiceInstall int64  `json:"service_install"`
	Name           string `json:"name"`
	Value          string `json:"value"`
}

// GetDeviceServiceEnvVar retrieves a single per-service device env var by ID.
func (c *Client) GetDeviceServiceEnvVar(ctx context.Context, id int64) (*DeviceServiceEnvVar, error) {
	return doGetByID[DeviceServiceEnvVar](ctx, c, "/v6/device_service_environment_variable", id)
}

// CreateDeviceServiceEnvVar creates a per-service env var on a device service install.
func (c *Client) CreateDeviceServiceEnvVar(ctx context.Context, serviceInstallID int64, name, value string) (*DeviceServiceEnvVar, error) {
	payload := deviceServiceEnvVarCreatePayload{ServiceInstall: serviceInstallID, Name: name, Value: value}
	return doCreate[DeviceServiceEnvVar](ctx, c, "/v6/device_service_environment_variable", payload)
}

// UpdateDeviceServiceEnvVar updates the value of a per-service device env var.
func (c *Client) UpdateDeviceServiceEnvVar(ctx context.Context, id int64, value string) error {
	return doPatch(ctx, c, "/v6/device_service_environment_variable", id, map[string]interface{}{"value": value})
}

// DeleteDeviceServiceEnvVar removes a per-service device env var by ID.
func (c *Client) DeleteDeviceServiceEnvVar(ctx context.Context, id int64) error {
	return doDelete(ctx, c, "/v6/device_service_environment_variable", id)
}

// SSHKey represents a user SSH key.
type SSHKey struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	PublicKey string `json:"public_key"`
	CreatedAt string `json:"created_at"`
}

type sshKeyCreatePayload struct {
	Title     string `json:"title"`
	PublicKey string `json:"public_key"`
}

// GetSSHKey retrieves a single SSH key by ID.
func (c *Client) GetSSHKey(ctx context.Context, id int64) (*SSHKey, error) {
	return doGetByID[SSHKey](ctx, c, "/v6/user__has__public_key", id)
}

// CreateSSHKey registers a new SSH public key for the authenticated user.
func (c *Client) CreateSSHKey(ctx context.Context, title, publicKey string) (*SSHKey, error) {
	payload := sshKeyCreatePayload{Title: title, PublicKey: publicKey}
	return doCreate[SSHKey](ctx, c, "/v6/user__has__public_key", payload)
}

// DeleteSSHKey removes an SSH key by ID.
func (c *Client) DeleteSSHKey(ctx context.Context, id int64) error {
	return doDelete(ctx, c, "/v6/user__has__public_key", id)
}

// ApplicationTag represents a tag on an application.
type ApplicationTag struct {
	ID     int64    `json:"id"`
	App    ODataRef `json:"application"`
	TagKey string   `json:"tag_key"`
	Value  string   `json:"value"`
}

type appTagCreatePayload struct {
	App    int64  `json:"application"`
	TagKey string `json:"tag_key"`
	Value  string `json:"value"`
}

// GetApplicationTag retrieves a single application tag by ID.
func (c *Client) GetApplicationTag(ctx context.Context, id int64) (*ApplicationTag, error) {
	return doGetByID[ApplicationTag](ctx, c, "/v6/application_tag", id)
}

// CreateApplicationTag creates a new tag on an application.
func (c *Client) CreateApplicationTag(ctx context.Context, appID int64, tagKey, value string) (*ApplicationTag, error) {
	payload := appTagCreatePayload{App: appID, TagKey: tagKey, Value: value}
	return doCreate[ApplicationTag](ctx, c, "/v6/application_tag", payload)
}

// UpdateApplicationTag updates the value of an application tag.
func (c *Client) UpdateApplicationTag(ctx context.Context, id int64, value string) error {
	return doPatch(ctx, c, "/v6/application_tag", id, map[string]interface{}{"value": value})
}

// DeleteApplicationTag removes an application tag by ID.
func (c *Client) DeleteApplicationTag(ctx context.Context, id int64) error {
	return doDelete(ctx, c, "/v6/application_tag", id)
}

// DeviceTag represents a tag on a device.
type DeviceTag struct {
	ID     int64    `json:"id"`
	Device ODataRef `json:"device"`
	TagKey string   `json:"tag_key"`
	Value  string   `json:"value"`
}

type deviceTagCreatePayload struct {
	Device int64  `json:"device"`
	TagKey string `json:"tag_key"`
	Value  string `json:"value"`
}

// GetDeviceTag retrieves a single device tag by ID.
func (c *Client) GetDeviceTag(ctx context.Context, id int64) (*DeviceTag, error) {
	return doGetByID[DeviceTag](ctx, c, "/v6/device_tag", id)
}

// CreateDeviceTag creates a new tag on a device.
func (c *Client) CreateDeviceTag(ctx context.Context, deviceID int64, tagKey, value string) (*DeviceTag, error) {
	payload := deviceTagCreatePayload{Device: deviceID, TagKey: tagKey, Value: value}
	return doCreate[DeviceTag](ctx, c, "/v6/device_tag", payload)
}

// UpdateDeviceTag updates the value of a device tag.
func (c *Client) UpdateDeviceTag(ctx context.Context, id int64, value string) error {
	return doPatch(ctx, c, "/v6/device_tag", id, map[string]interface{}{"value": value})
}

// DeleteDeviceTag removes a device tag by ID.
func (c *Client) DeleteDeviceTag(ctx context.Context, id int64) error {
	return doDelete(ctx, c, "/v6/device_tag", id)
}

// ServiceEnvVar represents a service-level environment variable.
type ServiceEnvVar struct {
	ID      int64    `json:"id"`
	Service ODataRef `json:"service"`
	Name    string   `json:"name"`
	Value   string   `json:"value"`
}

type serviceEnvVarCreatePayload struct {
	Service int64  `json:"service"`
	Name    string `json:"name"`
	Value   string `json:"value"`
}

// GetServiceEnvVar retrieves a single service environment variable by ID.
func (c *Client) GetServiceEnvVar(ctx context.Context, id int64) (*ServiceEnvVar, error) {
	return doGetByID[ServiceEnvVar](ctx, c, "/v6/service_environment_variable", id)
}

// CreateServiceEnvVar creates a new environment variable on a service.
func (c *Client) CreateServiceEnvVar(ctx context.Context, serviceID int64, name, value string) (*ServiceEnvVar, error) {
	payload := serviceEnvVarCreatePayload{Service: serviceID, Name: name, Value: value}
	return doCreate[ServiceEnvVar](ctx, c, "/v6/service_environment_variable", payload)
}

// UpdateServiceEnvVar updates the value of a service environment variable.
func (c *Client) UpdateServiceEnvVar(ctx context.Context, id int64, value string) error {
	return doPatch(ctx, c, "/v6/service_environment_variable", id, map[string]interface{}{"value": value})
}

// DeleteServiceEnvVar removes a service environment variable by ID.
func (c *Client) DeleteServiceEnvVar(ctx context.Context, id int64) error {
	return doDelete(ctx, c, "/v6/service_environment_variable", id)
}

// DeviceConfigVar represents a device-level configuration variable.
type DeviceConfigVar struct {
	ID     int64    `json:"id"`
	Device ODataRef `json:"device"`
	Name   string   `json:"name"`
	Value  string   `json:"value"`
}

type deviceConfigVarCreatePayload struct {
	Device int64  `json:"device"`
	Name   string `json:"name"`
	Value  string `json:"value"`
}

// GetDeviceConfigVar retrieves a single device config variable by ID.
func (c *Client) GetDeviceConfigVar(ctx context.Context, id int64) (*DeviceConfigVar, error) {
	return doGetByID[DeviceConfigVar](ctx, c, "/v6/device_config_variable", id)
}

// CreateDeviceConfigVar creates a new config variable on a device.
func (c *Client) CreateDeviceConfigVar(ctx context.Context, deviceID int64, name, value string) (*DeviceConfigVar, error) {
	payload := deviceConfigVarCreatePayload{Device: deviceID, Name: name, Value: value}
	return doCreate[DeviceConfigVar](ctx, c, "/v6/device_config_variable", payload)
}

// UpdateDeviceConfigVar updates the value of a device config variable.
func (c *Client) UpdateDeviceConfigVar(ctx context.Context, id int64, value string) error {
	return doPatch(ctx, c, "/v6/device_config_variable", id, map[string]interface{}{"value": value})
}

// DeleteDeviceConfigVar removes a device config variable by ID.
func (c *Client) DeleteDeviceConfigVar(ctx context.Context, id int64) error {
	return doDelete(ctx, c, "/v6/device_config_variable", id)
}

// ReleaseTag represents a tag on a release.
type ReleaseTag struct {
	ID      int64    `json:"id"`
	Release ODataRef `json:"release"`
	TagKey  string   `json:"tag_key"`
	Value   string   `json:"value"`
}

type releaseTagCreatePayload struct {
	Release int64  `json:"release"`
	TagKey  string `json:"tag_key"`
	Value   string `json:"value"`
}

// GetReleaseTag retrieves a single release tag by ID.
func (c *Client) GetReleaseTag(ctx context.Context, id int64) (*ReleaseTag, error) {
	return doGetByID[ReleaseTag](ctx, c, "/v6/release_tag", id)
}

// CreateReleaseTag creates a new tag on a release.
func (c *Client) CreateReleaseTag(ctx context.Context, releaseID int64, tagKey, value string) (*ReleaseTag, error) {
	payload := releaseTagCreatePayload{Release: releaseID, TagKey: tagKey, Value: value}
	return doCreate[ReleaseTag](ctx, c, "/v6/release_tag", payload)
}

// UpdateReleaseTag updates the value of a release tag.
func (c *Client) UpdateReleaseTag(ctx context.Context, id int64, value string) error {
	return doPatch(ctx, c, "/v6/release_tag", id, map[string]interface{}{"value": value})
}

// DeleteReleaseTag removes a release tag by ID.
func (c *Client) DeleteReleaseTag(ctx context.Context, id int64) error {
	return doDelete(ctx, c, "/v6/release_tag", id)
}

// ImageEnvVar represents an image-level environment variable.
type ImageEnvVar struct {
	ID           int64    `json:"id"`
	ReleaseImage ODataRef `json:"release_image"`
	Name         string   `json:"name"`
	Value        string   `json:"value"`
}

type imageEnvVarCreatePayload struct {
	ReleaseImage int64  `json:"release_image"`
	Name         string `json:"name"`
	Value        string `json:"value"`
}

// GetImageEnvVar retrieves a single image environment variable by ID.
func (c *Client) GetImageEnvVar(ctx context.Context, id int64) (*ImageEnvVar, error) {
	return doGetByID[ImageEnvVar](ctx, c, "/v6/image_environment_variable", id)
}

// CreateImageEnvVar creates a new environment variable on a release image.
func (c *Client) CreateImageEnvVar(ctx context.Context, releaseImageID int64, name, value string) (*ImageEnvVar, error) {
	payload := imageEnvVarCreatePayload{ReleaseImage: releaseImageID, Name: name, Value: value}
	return doCreate[ImageEnvVar](ctx, c, "/v6/image_environment_variable", payload)
}

// UpdateImageEnvVar updates the value of an image environment variable.
func (c *Client) UpdateImageEnvVar(ctx context.Context, id int64, value string) error {
	return doPatch(ctx, c, "/v6/image_environment_variable", id, map[string]interface{}{"value": value})
}

// DeleteImageEnvVar removes an image environment variable by ID.
func (c *Client) DeleteImageEnvVar(ctx context.Context, id int64) error {
	return doDelete(ctx, c, "/v6/image_environment_variable", id)
}

// ServiceLabel represents a label on a service.
type ServiceLabel struct {
	ID        int64    `json:"id"`
	Service   ODataRef `json:"service"`
	LabelName string   `json:"label_name"`
	Value     string   `json:"value"`
}

type serviceLabelCreatePayload struct {
	Service   int64  `json:"service"`
	LabelName string `json:"label_name"`
	Value     string `json:"value"`
}

// GetServiceLabel retrieves a single service label by ID.
func (c *Client) GetServiceLabel(ctx context.Context, id int64) (*ServiceLabel, error) {
	return doGetByID[ServiceLabel](ctx, c, "/v6/service_label", id)
}

// CreateServiceLabel creates a new label on a service.
func (c *Client) CreateServiceLabel(ctx context.Context, serviceID int64, labelName, value string) (*ServiceLabel, error) {
	payload := serviceLabelCreatePayload{Service: serviceID, LabelName: labelName, Value: value}
	return doCreate[ServiceLabel](ctx, c, "/v6/service_label", payload)
}

// UpdateServiceLabel updates the value of a service label.
func (c *Client) UpdateServiceLabel(ctx context.Context, id int64, value string) error {
	return doPatch(ctx, c, "/v6/service_label", id, map[string]interface{}{"value": value})
}

// DeleteServiceLabel removes a service label by ID.
func (c *Client) DeleteServiceLabel(ctx context.Context, id int64) error {
	return doDelete(ctx, c, "/v6/service_label", id)
}

// Service (read-only, for data source)

// Service represents a Balena service resource.
type Service struct {
	ID          int64    `json:"id"`
	App         ODataRef `json:"application"`
	ServiceName string   `json:"service_name"`
}

// GetServiceByName retrieves a service by application ID and service name.
func (c *Client) GetServiceByName(ctx context.Context, appID int64, serviceName string) (*Service, error) {
	filter := "$filter=" + url.QueryEscape(fmt.Sprintf("application eq %d and service_name eq '%s'", appID, escapeOData(serviceName)))
	items, err := doListFiltered[Service](ctx, c, "/v6/service", filter)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, &APIError{StatusCode: http.StatusNotFound, Body: fmt.Sprintf("service %q not found in application %d", serviceName, appID)}
	}
	return &items[0], nil
}

// Release (read-only, for data source)

// Release represents a Balena release resource.
type Release struct {
	ID             int64    `json:"id"`
	App            ODataRef `json:"belongs_to__application"`
	Commit         string   `json:"commit"`
	Status         string   `json:"status"`
	ReleaseVersion string   `json:"release_version"`
	Semver         string   `json:"semver"`
	CreatedAt      string   `json:"created_at"`
}

// GetRelease retrieves a single release by ID.
func (c *Client) GetRelease(ctx context.Context, id int64) (*Release, error) {
	return doGetByID[Release](ctx, c, "/v6/release", id)
}

// GetReleaseByCommit retrieves a release by application ID and commit hash.
func (c *Client) GetReleaseByCommit(ctx context.Context, appID int64, commit string) (*Release, error) {
	filter := "$filter=" + url.QueryEscape(fmt.Sprintf("belongs_to__application eq %d and commit eq '%s'", appID, escapeOData(commit)))
	items, err := doListFiltered[Release](ctx, c, "/v6/release", filter)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, &APIError{StatusCode: http.StatusNotFound, Body: fmt.Sprintf("release with commit %q not found in application %d", commit, appID)}
	}
	return &items[0], nil
}

// Organization (read-only, for data source)

// Organization represents a Balena organization resource.
type Organization struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Handle string `json:"handle"`
}

type organizationCreatePayload struct {
	Name   string `json:"name"`
	Handle string `json:"handle,omitempty"`
}

// GetOrganization retrieves a single organization by ID.
func (c *Client) GetOrganization(ctx context.Context, id int64) (*Organization, error) {
	return doGetByID[Organization](ctx, c, "/v6/organization", id)
}

// GetOrganizationByHandle retrieves a single organization by its handle.
func (c *Client) GetOrganizationByHandle(ctx context.Context, handle string) (*Organization, error) {
	filter := "$filter=" + url.QueryEscape("handle eq '"+escapeOData(handle)+"'")
	items, err := doListFiltered[Organization](ctx, c, "/v6/organization", filter)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, &APIError{StatusCode: http.StatusNotFound, Body: fmt.Sprintf("organization with handle %q not found", handle)}
	}
	return &items[0], nil
}

// CreateOrganization creates a new organization.
func (c *Client) CreateOrganization(ctx context.Context, name, handle string) (*Organization, error) {
	payload := organizationCreatePayload{Name: name, Handle: handle}
	return doCreate[Organization](ctx, c, "/v6/organization", payload)
}

// UpdateOrganization patches an existing organization.
func (c *Client) UpdateOrganization(ctx context.Context, id int64, body map[string]interface{}) error {
	return doPatch(ctx, c, "/v6/organization", id, body)
}

// DeleteOrganization removes an organization by ID.
func (c *Client) DeleteOrganization(ctx context.Context, id int64) error {
	return doDelete(ctx, c, "/v6/organization", id)
}

// escapeOData escapes single quotes in OData string literals.
func escapeOData(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func (c *Client) newRequest(ctx context.Context, method, rawPath string, body interface{}) (*http.Request, error) {
	// rawPath may contain query strings (e.g. "/v6/app?$filter=...") or
	// OData key predicates (e.g. "/v6/app(123)"), so we concatenate directly.
	u := c.baseURL + rawPath

	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshalling request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	return req, nil
}

func (c *Client) do(req *http.Request) ([]byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(data)}
	}
	return data, nil
}

func doList[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	data, err := c.do(req)
	if err != nil {
		return nil, err
	}
	var resp pineResponse[T]
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return resp.D, nil
}

func doListFiltered[T any](ctx context.Context, c *Client, path, filter string) ([]T, error) {
	req, err := c.newRequest(ctx, http.MethodGet, path+"?"+filter, nil)
	if err != nil {
		return nil, err
	}
	data, err := c.do(req)
	if err != nil {
		return nil, err
	}
	var resp pineResponse[T]
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return resp.D, nil
}

func doGetByID[T any](ctx context.Context, c *Client, path string, id int64, query ...string) (*T, error) {
	u := path + "(" + strconv.FormatInt(id, 10) + ")"
	if len(query) > 0 {
		u += "?" + strings.Join(query, "&")
	}
	req, err := c.newRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	data, err := c.do(req)
	if err != nil {
		return nil, err
	}
	var resp pineResponse[T]
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	if len(resp.D) == 0 {
		return nil, &APIError{
			StatusCode: http.StatusNotFound,
			Body:       fmt.Sprintf("resource with id %d not found", id),
		}
	}
	return &resp.D[0], nil
}

func doCreate[T any](ctx context.Context, c *Client, path string, body interface{}) (*T, error) {
	var data []byte
	var err error

	for attempt := 0; ; attempt++ {
		var req *http.Request
		req, err = c.newRequest(ctx, http.MethodPost, path, body)
		if err != nil {
			return nil, err
		}
		data, err = c.do(req)
		// Retry on 409 Conflict — a concurrent delete of the same resource
		// (e.g. Terraform renaming a resource) may not have propagated yet.
		if err != nil && IsConflict(err) && attempt < 3 {
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
			continue
		}
		break
	}

	if err != nil {
		return nil, err
	}
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decoding create response: %w", err)
	}
	return &result, nil
}

func doPatch(ctx context.Context, c *Client, path string, id int64, body interface{}) error {
	req, err := c.newRequest(ctx, http.MethodPatch, path+"("+strconv.FormatInt(id, 10)+")", body)
	if err != nil {
		return err
	}
	_, err = c.do(req)
	return err
}

func doDelete(ctx context.Context, c *Client, path string, id int64) error {
	req, err := c.newRequest(ctx, http.MethodDelete, path+"("+strconv.FormatInt(id, 10)+")", nil)
	if err != nil {
		return err
	}
	_, err = c.do(req)
	return err
}
