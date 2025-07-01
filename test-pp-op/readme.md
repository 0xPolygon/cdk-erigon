# How to run
```shell
make build-docker;
make run; 


cast send -f 0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534  --private-key 0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2 --value 0.01ether 0xA6f7A6b2E9B4d41C582D4Aaf907F45321e2Ca847 --legacy --rpc-url http://127.0.0.1:8124
```

# How to use bridge
```
make all;

http://127.0.0.1:8090/
L1 OKB Token: 0x5FbDB2315678afecb367f032d93F642f64180aa3
L2 WETH Token: 0xd80e5a44dc9628fae9b432eac67873238504ea29
L2 admin: 0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534

```
