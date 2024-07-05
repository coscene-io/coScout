package constant

const (
	RegisterProviderGs   = "gs"
	RegisterProviderAgi  = "agi"
	RegisterProviderFile = "file"
	RegisterProviderAuto = "auto"

	// AuthHeaderKey request header key.
	AuthHeaderKey = "Authorization"

	DeviceAuthBucket          = "device_auth"
	DeviceAuthKey             = "api_token"
	DeviceAuthExpireKey       = "api_token_expire"
	DeviceAuthExchangeCodeKey = "exchange_code"

	DeviceMetadataBucket = "device_metadata"
	DeviceInfoKey        = "device_info"
)
