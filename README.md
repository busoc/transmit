# transmit

enhance packets transmission from one network to another.

## transmit protocol

## transmit relay

### table [default]

* remote: remote address of the host where to connect to forward packets

### table [certificate]

* pem-file: path to a pem encoded certificate
* key-file: path to a pem encoded certificate key
* cert-auth: list of certificates to be used to verify server certificate
* insecure: verify the server certificate chain and its hostname

### table [[route]]

* id:
* ip:

### configuration example

```toml
remote = "localhost:11111"

[certificate]
pem-file  = "relay.cert"
key-file  = "relay.key"
cert-auth = [] # path(s) to certificate used to validate server certificate
insecure  = true

[[route]]
id = 41001
ip = "239.192.0.1:33333"

[[route]]
id = 41002
ip = "239.192.0.1:44444"
```

## transmit gateway

### table [default]

* local: local address to be used for accepting connection from clients
* clients: maximum number of simulatenous client connections accepted

### table [certificate]

* pem-file: path to a pem encoded certificate
* key-file: path to a pem encoded certificate key
* cert-auth: list of certificates to be used to verify client certificates
* policy: policy for TLS client authentication

### table [[route]]

* port:
* ip:

### example: configuration

```toml
local   = "0.0.0.0:11111"
clients = 12

[certificate]
pem-file  = "gateway.cert"
key-file  = "gateway.key"
policy    = "require+verify"
cert-auth = []

[[route]]
id = 41001
ip = "239.192.0.1:44444"

[[route]]
id = 41002
ip = "239.192.0.1:33333"
```

## transmit feed (alias: sim, play, test)
