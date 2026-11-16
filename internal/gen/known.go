package gen

// The SBVR files are not the whole model: open-balena-api renames some columns and adds computed fields and a view in
// TypeScript after compiling the SBVR (src/balena.ts, src/abstract-sql-utils.ts, src/features/**/models/*-additions.ts).
// These maps are the only accepted explanations for a difference between an SBVR file and the TypeScript model of the
// same root. Anything else fails the cross-check.

// KNOWN_RENAMES maps "<table>.<sbvr field>" to the column name the TypeScript model uses.
var KNOWN_RENAMES = map[string]string{
	"application_environment_variable.env_var_name": "name",
	"device_environment_variable.env_var_name":      "name",
	"application_config_variable.config_var_name":   "name",
	"device_config_variable.config_var_name":        "name",
}

// KNOWN_COMPUTED lists "<table>.<field>" columns that open-balena-api computes in code. They are read-only.
var KNOWN_COMPUTED = map[string]bool{
	"device.overall_status":             true,
	"device.overall_progress":           true,
	"device.is_web_accessible":          true,
	"device.is_frozen":                  true,
	"device.should_be_running__release": true,
	"release.is_final":                  true,
	"release.semver":                    true,
	"release.raw_version":               true,
	"release.version":                   true,
	"application_profile_catalog.available_since__release": true,
}

// KNOWN_VIEWS lists tables open-balena-api adds in code (no SBVR fact type).
var KNOWN_VIEWS = map[string]bool{
	"user__has_direct_access_to__application": true,
}

// AUTO_FIELDS are the columns every table has and that the server always sets.
var AUTO_FIELDS = map[string]bool{
	"id":          true,
	"created_at":  true,
	"modified_at": true,
}

// KNOWN_TYPE_CHANGES maps "<table>.<field>" to the type open-balena-api gives a column in code instead of the SBVR type
// (v7.sbvr: "The release.version's field type is overriden to JSON by changing the generated abstract sql").
var KNOWN_TYPE_CHANGES = map[string]string{
	"release.version": "JSON",
}

// KNOWN_CLOUD_OMISSIONS lists SBVR tables ("<table>") and columns ("<table>.<field>") that balena-sdk's typings of a
// balena-cloud root leave out because users cannot read them: internal authorisation tables and secrets.
var KNOWN_CLOUD_OMISSIONS = map[string]string{
	"api_key__has__permission":        "internal authorisation table",
	"api_key__has__role":              "internal authorisation table",
	"config":                          "internal configuration table",
	"permission":                      "internal authorisation table",
	"role":                            "internal authorisation table",
	"role__has__permission":           "internal authorisation table",
	"user__has__permission":           "internal authorisation table",
	"user__has__role":                 "internal authorisation table",
	"api_key.key":                     "secret: the key is only returned when it is created",
	"device.api_port":                 "internal: used by the API to reach the supervisor",
	"device.api_secret":               "secret: used by the API to reach the supervisor",
	"service_instance.last_heartbeat": "internal VPN bookkeeping",
	"service_instance.service_type":   "internal VPN bookkeeping",
	"user.email":                      "personal data the cloud model hides",
	"user.jwt_secret":                 "secret",
	"user.password":                   "secret",
}
