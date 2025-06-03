USE `ApolloPortalDB`;

INSERT INTO `App` (`AppId`, `Name`, `OrgId`, `OrgName`, `OwnerName`, `OwnerEmail`)
VALUES ('XLayerSeq', 'XLayerSeq', 'default', 'Default', 'apollo', 'apollo@example.com'),
       ('XLayerRpc', 'XLayerRpc', 'default', 'Default', 'apollo', 'apollo@example.com');

INSERT INTO `AppNamespace` (`Name`, `AppId`, `Format`, `IsPublic`, `Comment`)
VALUES ('jsonrpc-config.txt', 'XLayerSeq', 'txt', 0, 'jsonrpc config for seq'),
       ('sequencer-config.txt', 'XLayerSeq', 'txt', 0, 'sequencer config for seq'),
       ('l2gaspricer-config.txt', 'XLayerSeq', 'txt', 0, 'l2 gas pricer config for seq'),
       ('pool-config.txt', 'XLayerSeq', 'txt', 0, 'pool config for seq'),
       ('jsonrpc-rpc-config.txt', 'XLayerRpc', 'txt', 0, 'jsonrpc config for rpc');

INSERT INTO `Permission` (`Id`, `PermissionType`, `TargetId`)
VALUES (1, 'CreateCluster', 'XLayerSeq'),
       (2, 'CreateNamespace', 'XLayerSeq'),
       (3, 'AssignRole', 'XLayerSeq'),
       (4, 'ModifyNamespace', 'XLayerSeq+jsonrpc-config.txt'),
       (5, 'ReleaseNamespace', 'XLayerSeq+jsonrpc-config.txt'),
       (6, 'ModifyNamespace', 'XLayerSeq+sequencer-config.txt'),
       (7, 'ReleaseNamespace', 'XLayerSeq+sequencer-config.txt'),
       (8, 'ModifyNamespace', 'XLayerSeq+l2gaspricer-config.txt'),
       (9, 'ReleaseNamespace', 'XLayerSeq+l2gaspricer-config.txt'),
       (10, 'ModifyNamespace', 'XLayerSeq+pool-config.txt'),
       (11, 'ReleaseNamespace', 'XLayerSeq+pool-config.txt'),
       (12, 'CreateCluster', 'XLayerRpc'),
       (13, 'CreateNamespace', 'XLayerRpc'),
       (14, 'AssignRole', 'XLayerRpc'),
       (15, 'ModifyNamespace', 'XLayerRpc+jsonrpc-rpc-config.txt'),
       (16, 'ReleaseNamespace', 'XLayerRpc+jsonrpc-rpc-config.txt');

INSERT INTO `Role` (`Id`, `RoleName`)
VALUES (1, 'Master+XLayerSeq'),
       (2, 'ModifyNamespace+XLayerSeq+jsonrpc-config.txt'),
       (3, 'ReleaseNamespace+XLayerSeq+jsonrpc-config.txt'),
       (4, 'ModifyNamespace+XLayerSeq+sequencer-config.txt'),
       (5, 'ReleaseNamespace+XLayerSeq+sequencer-config.txt'),
       (6, 'ModifyNamespace+XLayerSeq+l2gaspricer-config.txt'),
       (7, 'ReleaseNamespace+XLayerSeq+l2gaspricer-config.txt'),
       (8, 'ModifyNamespace+XLayerSeq+pool-config.txt'),
       (9, 'ReleaseNamespace+XLayerSeq+pool-config.txt'),
       (10, 'Master+XLayerRpc'),
       (11, 'ModifyNamespace+XLayerRpc+jsonrpc-rpc-config.txt'),
       (12, 'ReleaseNamespace+XLayerRpc+jsonrpc-rpc-config.txt');

INSERT INTO `RolePermission` (`RoleId`, `PermissionId`)
VALUES (1, 1),
       (1, 2),
       (1, 3),
       (2, 4),
       (3, 5),
       (4, 6),
       (5, 7),
       (6, 8),
       (7, 9),
       (8, 10),
       (9, 11),
       (10, 12),
       (10, 13),
       (10, 14),
       (11, 15),
       (12, 16);

INSERT INTO `UserRole` (`UserId`, `RoleId`)
VALUES ('apollo', 1),
       ('apollo', 2),
       ('apollo', 3),
       ('apollo', 4),
       ('apollo', 5),
       ('apollo', 6),
       ('apollo', 7),
       ('apollo', 8),
       ('apollo', 9),
       ('apollo', 10),
       ('apollo', 11),
       ('apollo', 12);

USE `ApolloConfigDB`;

INSERT INTO `App` (`AppId`, `Name`, `OrgId`, `OrgName`, `OwnerName`, `OwnerEmail`)
VALUES ('XLayerSeq', 'XLayerSeq', 'default', 'default', 'apollo', 'apollo@example.com'),
       ('XLayerRpc', 'XLayerRpc', 'default', 'Default', 'apollo', 'apollo@example.com');

INSERT INTO `Cluster` (`Name`, `AppId`, `IsDeleted`)
VALUES ('default', 'XLayerSeq', 0),
       ('default', 'XLayerRpc', 0);

INSERT INTO `AppNamespace` (`Name`, `AppId`, `Format`, `IsPublic`, `Comment`)
VALUES ('jsonrpc-config.txt', 'XLayerSeq', 'txt', 0, 'jsonrpc config for seq'),
       ('sequencer-config.txt', 'XLayerSeq', 'txt', 0, 'sequencer config for seq'),
       ('l2gaspricer-config.txt', 'XLayerSeq', 'txt', 0, 'l2 gas pricer config for seq'),
       ('pool-config.txt', 'XLayerSeq', 'txt', 0, 'pool config for seq'),
       ('jsonrpc-rpc-config.txt', 'XLayerRpc', 'txt', 0, 'jsonrpc config for rpc');

INSERT INTO `Namespace` (`AppId`, `ClusterName`, `NamespaceName`, `IsDeleted`)
VALUES ('XLayerSeq', 'default', 'jsonrpc-config.txt', 0), 
       ('XLayerSeq', 'default', 'sequencer-config.txt', 0),
       ('XLayerSeq', 'default', 'l2gaspricer-config.txt', 0),
       ('XLayerSeq', 'default', 'pool-config.txt', 0),
       ('XLayerRpc', 'default', 'jsonrpc-rpc-config.txt', 0);

INSERT INTO `Item` (`NamespaceId`, `Key`, `Type`, `Value`, `IsDeleted`)
VALUES ('2', 'content', 0, 'zkevm.sequencer-batch-sleep-duration: 1s\nzkevm.sequencer-block-seal-time: 2s\nzkevm.sequencer-batch-seal-time: 20s\nzkevm.bulk-add-txs: false\nyieldsize: 50\nzkevm.enable-add-tx-notify: false\nzkevm.pre-run-address-list: ["0xa03666Fb51Aa9aD2DE70e0434072A007b3C91A9E"]\nzkevm.block-info-concurrent: false\nzkevm.sequencer-batch-counter-percentage: 50\nzkevm.sequencer-max-block-seal-time: 2s\nzkevm.get-logs-timeout: 10s\nzkevm.get-logs-retries: 3\n', 0),
       ('4', 'content', 0, 'txpool.enabletimsort: false\ntxpool.packbatchspeciallist: ["0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266", "0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534"]\ntxpool.blockedlist: ["0xdD2FD4581271e230360230F9337D5c0430Bf44C0"]\ntxpool.enablefreegaslist: true\ntxpool.freegaslist: ''[{\"name\":\"e2e\",\"from_list\":[\"0x586cbc95ed16031d9efdaebacf1c1d2bc3ccaa78\"],\"to_list\":[\"0xad1d01007a56ee0a4ffd0488fb58fc6500cb1fbe\"],\"method_sigs\":[\"a9059cbb\"],\"gas_price_multiple\":1}]''\n', 0),
       ('5', 'content', 0, 'http.enabled: false', 0);

