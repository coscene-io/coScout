package constant

const (
	RegisterProviderFile = "file"
	RegisterProviderAuto = "auto"

	TaskHandlerType = "task"

	// AuthHeaderKey request header key.
	AuthHeaderKey     = "Authorization"
	BasicAuthPrefix   = "Basic"
	BasicAuthUsername = "apikey"

	DeviceAuthBucket          = "device_auth"
	DeviceAuthKey             = "api_token"
	DeviceAuthExpireKey       = "api_token_expire"
	DeviceAuthExchangeCodeKey = "exchange_code"

	DeviceMetadataBucket = "device_metadata"
	DeviceInfoKey        = "device_info"

	// DeviceRemoteCacheBucket Remote Config cache key
	DeviceRemoteCacheBucket = "device_remote_cache"

	DeviceRemoteConfigBucket = "device_remote_config"

	// FileInfoBucket File info bucket
	FileInfoBucket = "file_info_bucket"

	// MultiPartUploadBucket Multi part upload bucket
	MultiPartUploadBucket = "multi_part_upload_bucket"

	UploadBucket = "default"

	LabelUploadSuccess = "上传完成"
)
