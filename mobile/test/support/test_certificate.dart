// A throwaway self-signed ECDSA P-256 certificate and key, used only by the TLS pinning tests.
// Generated with openssl; it protects nothing.

const testCertificatePem = '''
-----BEGIN CERTIFICATE-----
MIIBYjCCAQigAwIBAgIUGgZgiWoynmwEOveINEGqAufsk2YwCgYIKoZIzj0EAwIw
FjEUMBIGA1UEAwwLcG91cGxlLXRlc3QwIBcNMjYxMTIzMDgzMzA5WhgPMjEyNjEw
MzAwODMzMDlaMBYxFDASBgNVBAMMC3BvdXBsZS10ZXN0MFkwEwYHKoZIzj0CAQYI
KoZIzj0DAQcDQgAEYF680DEY5y70Hy1EhcUcE4OS5PcOT8Pe2Py3sz8EK8ef2wEW
jSh4ULTRmh+3bx67CsUGZ/AhOWFYk2NrRPmawaMyMDAwHQYDVR0OBBYEFC9VdShr
4ytrpbpuq7ylrQQMq4I7MA8GA1UdEwEB/wQFMAMBAf8wCgYIKoZIzj0EAwIDSAAw
RQIhAIxAWghi3ThKs+d5G2OzIedIabTWgIPZnId6wmuDRweMAiB7MQk1G2TvKEI4
80pDrZj4hEkCcq17jTqGrUT7lhCV2A==
-----END CERTIFICATE-----
''';

const testPrivateKeyPem = '''
-----BEGIN PRIVATE KEY-----
MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgVx/qhqfkxuHlXICb
AfQa67oEe/G1JdbPkcY7LjQ0juGhRANCAARgXrzQMRjnLvQfLUSFxRwTg5Lk9w5P
w97Y/LezPwQrx5/bARaNKHhQtNGaH7dvHrsKxQZn8CE5YViTY2tE+ZrB
-----END PRIVATE KEY-----
''';

/// SHA-256 of the certificate's DER, lowercase hex.
const testCertificateFingerprint = 'e4cfd0f013115dbf51f33507eeeeaadcfa1c9d0b5e9e968e1cafb51c806cd9f3';
