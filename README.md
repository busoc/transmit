# transmit

## transmit protocol

To forward packets from one network to another, transmit uses a simple protocol
by adding its own headers on each received packets.

| header | size (bytes) | description                             |
| ------ | ------------ | --------------------------------------- |
| size   | 2            | size of the original packet in bytes    |
| id     | 2            | identifier of the remote route          |
| digest | 8            | checksum (xxh64) of the original packet |

The goal of the protocol is to ensure two things:

1. all packets are correctly re-routed to the correct network after having been
   forwarded
2. no corruption occurred between the source network and the destination network

## transmit relay

The relay sub command is used to forward packets from one or multiple multicast
groups to another network that does not have any connectivity with its source (
eg, firewall between the two networks only accepting a limited set of TCP
connections).

The task of this command is to connect to the remote network and "prepare" all
incoming packets so that they can be re-routed properly in the remote network.

### table [default]

* remote: remote address of the host where to connect to forward packets

### table [certificate]

* pem-file: path to a PEM encoded certificate
* key-file: path to a PEM encoded certificate key
* cert-auth: list of certificates to be used to verify server certificate
* insecure: verify the server certificate chain and its host name

### table [[route]]

* id: a number used to identify the remote route where transmit will forward the
  incoming stream. If not set or set to 0, the port number of the source address
  will be used.
* ip: address (host:port) where to listen for incoming packets

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

The gateway sub command does the opposite of the relay sub command. It unwrap the
original packet, checks its header and try to find the route where the packet should
be sent regarding the "id" field of the header. If no route is found, the packets
with the given id are automatically discarded by transmit.

### table [default]

* local: local address to be used for accepting connection from clients
* clients: maximum number of simultaneous client connections accepted

### table [certificate]

* pem-file: path to a PEM encoded certificate
* key-file: path to a PEM encoded certificate key
* cert-auth: list of certificates to be used to verify client certificates
* policy: policy for TLS client authentication

### table [[route]]

* id: a number used to identify the route where to forward the incoming packets
  stream. If not set or set to 0, the port number of the destination address will
  be used
* ip: address (host:port) where to transmit will forward incoming packets

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

## transmit feed

The feed sub command is mainly used to send dummy packets (filled of zeros or with
random data) to a transmit relay instance. But it can also be used to send dummy
packets to another process such as [duplicate](https://github.com/busoc/duplicate)
for test purposes.

```bash
$ transmit (feed|sim|play|test) [options...] <host:port>

where options are:

  -z        create packets filled of zero. If not set, the default, packets will contain random data
  -s SIZE   create packets of SIZE bytes
  -c COUNT  send COUNT packets and then quit. if COUNT is zero, then feed will continue forever
  -p SLEEP  wait SLEEP time between two packets
  -h        show the help message and exit
```

## transmit log

The log sub command can be used to get basic information about the incoming packets.
It prints on stderr the following information:

* timestamp (YYYY/MM/DD hh:mm:ss)
* size of each packets received (in bytes)
* first 16 bytes of each packets
* checksum of each packets (computed with xxHash)

```bash
$ transmit (log|dump) [options...] <host:port...>

where options are:

  -c COUNT  log information for the COUNT first packets
  -h        show the help message and exit
```


## transmit store

The store sub command can be used to write each incoming packets in a file. Each
packet written will be preceded by two fields added by transmit:

* size: unsigned integer 32bits big endian
* unix timestamp: signed integer 64bits big endian

```bash
$ transmit store [options...] <host:port...>

where options are:

  -c COUNT    log information for the COUNT first packets
  -p PREFIX   use PREFIX as prefix of each filename where packets will be written
  -d DATADIR  save files in DATADIR creating it if necessary
  -h          show the help message and exit
```
