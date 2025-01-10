// Copyright 2025 coScene
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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

	// DeviceRemoteCacheBucket Remote Config cache key.
	DeviceRemoteCacheBucket = "device_remote_cache"

	DeviceRemoteConfigBucket = "device_remote_config"

	// FileInfoBucket File info bucket.
	FileInfoBucket = "file_info_bucket"

	// MultiPartUploadBucket Multi part upload bucket.
	MultiPartUploadBucket = "multi_part_upload_bucket"

	UploadBucket = "default"

	LabelUploadSuccess = "\u4E0A\u4F20\u5B8C\u6210"

	TaskModType = "task"
	RuleModType = "rule"
)
