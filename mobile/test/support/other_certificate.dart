// A second throwaway self-signed certificate, for tests where a different certificate answers.

const otherCertificatePem = '''
-----BEGIN CERTIFICATE-----
MIIBYzCCAQqgAwIBAgIUf6s/MOyjEGDYZNMOlcZo1jewvZwwCgYIKoZIzj0EAwIw
FzEVMBMGA1UEAwwMcG91cGxlLW90aGVyMCAXDTI2MTEyMzA4NTgxMVoYDzIxMjYx
MDMwMDg1ODExWjAXMRUwEwYDVQQDDAxwb3VwbGUtb3RoZXIwWTATBgcqhkjOPQIB
BggqhkjOPQMBBwNCAASV06aBZLvS8jhfe+MnQTTUQ0wfncd0p5EXwjbvtxI3cp71
usrZsZEOy4/sfVXr5v3NIezRpx0VbYHWS9S27OTVozIwMDAdBgNVHQ4EFgQUmrSU
A0snNIlHVIhSYrCVtbSMUQswDwYDVR0TAQH/BAUwAwEB/zAKBggqhkjOPQQDAgNH
ADBEAiB6RQtyGj9L4V2+ZxV7hyQZbQGw9al10aoBFRbyJbEWgAIgcECltJf+Ea2M
0R6O/Q6EqzYm4XNwl/Ba0V73dejDqgM=
-----END CERTIFICATE-----
''';

const otherPrivateKeyPem = '''
-----BEGIN PRIVATE KEY-----
MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgPUnzthV0cZhPUrjH
04dogYu/IKeGlgfN5pSAiiENFfShRANCAASV06aBZLvS8jhfe+MnQTTUQ0wfncd0
p5EXwjbvtxI3cp71usrZsZEOy4/sfVXr5v3NIezRpx0VbYHWS9S27OTV
-----END PRIVATE KEY-----
''';

const otherCertificateFingerprint = '35e2d72603a857cd94d9542438ed78ba7eff9776fed5d486aea011d1933114c8';
