# Scan report — `/home/sveinn/code/eos-fork`

- scan /home/sveinn/code/eos-fork — 151 findings
- Generated at: 14:35:19
- Tool: code-analyzer

## potential data races (93)

1. **RACE** **`internal/jstream/scanner.go:38`** — potential data race on `sr` — captured by goroutine without synchronization
   - goroutine launched here (`internal/jstream/scanner.go:34`)
   - written inside the goroutine (`internal/jstream/scanner.go:38`)
   - read outside the goroutine (`internal/jstream/scanner.go:63`)

2. **RACE WARN** **`internal/ioutil/ioutil.go:306`** — potential data race on `r` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`internal/ioutil/ioutil.go:303`)
   - written outside the goroutine (`internal/ioutil/ioutil.go:352`)
   - the branches guarding the launch and this access may be mutually exclusive — racy only if a code change lets them overlap

3. **RACE WARN** **`internal/dsync/drwmutex.go:709`** — potential data race on `locks` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`internal/dsync/drwmutex.go:707`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`internal/dsync/drwmutex.go:705`)
   - written inside the goroutine (`internal/dsync/drwmutex.go:710`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

4. **RACE WARN** **`internal/console/handlers.go:4126`** — potential data race on `job` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`internal/console/handlers.go:4119`)
   - written inside the goroutine (`internal/console/handlers.go:4127`)
   - written outside the goroutine (`internal/console/handlers.go:4204`)
   - both accesses appear guarded by the same mutex (lock pairing not verified)

5. **RACE WARN** **`internal/kms/kes.go:139`** — potential data race on `results` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`internal/kms/kes.go:125`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`internal/kms/kes.go:123`)
   - written inside the goroutine (`internal/kms/kes.go:139`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

6. **RACE** **`internal/grid/muxserver.go:112`** — potential data race on `m` — captured by goroutine without synchronization
   - goroutine launched here (`internal/grid/muxserver.go:108`)
   - written outside the goroutine (`internal/grid/muxserver.go:173`)

7. **RACE** **`internal/grid/muxserver.go:137`** — potential data race on `m` — captured by goroutine without synchronization
   - goroutine launched here (`internal/grid/muxserver.go:127`)
   - written outside the goroutine (`internal/grid/muxserver.go:173`)

8. **RACE** **`internal/grid/muxserver.go:153`** — potential data race on `m` — captured by goroutine without synchronization
   - goroutine launched here (`internal/grid/muxserver.go:149`)
   - written outside the goroutine (`internal/grid/muxserver.go:173`)

9. **RACE** **`internal/grid/muxserver.go:162`** — potential data race on `m` — captured by goroutine without synchronization
   - goroutine launched here (`internal/grid/muxserver.go:160`)
   - written outside the goroutine (`internal/grid/muxserver.go:173`)

10. **RACE** **`internal/grid/muxserver.go:170`** — potential data race on `m` — captured by goroutine without synchronization
   - goroutine launched here (`internal/grid/muxserver.go:168`)
   - written outside the goroutine (`internal/grid/muxserver.go:173`)

11. **RACE** **`internal/grid/connection.go:313`** — potential data race on `c` — captured by goroutine without synchronization
   - goroutine launched here (`internal/grid/connection.go:309`)
   - written outside the goroutine (`internal/grid/connection.go:323`)

12. **RACE WARN** **`internal/grid/connection.go:2116`** — potential data race on `c` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`internal/grid/connection.go:2114`)
   - written outside the goroutine (`internal/grid/connection.go:2122`)
   - the branches guarding the launch and this access may be mutually exclusive — racy only if a code change lets them overlap

13. **RACE** **`internal/inventory/system.go:136`** — potential data race on `mgr` — captured by goroutine without synchronization
   - goroutine launched here (`internal/inventory/system.go:135`)
   - written outside the goroutine (`internal/inventory/system.go:146`)

14. **RACE** **`internal/inventory/system.go:144`** — potential data race on `mgr` — captured by goroutine without synchronization
   - goroutine launched here (`internal/inventory/system.go:143`)
   - written inside the goroutine (`internal/inventory/system.go:146`)
   - written outside the goroutine (`internal/inventory/system.go:150`)

15. **RACE** **`cmd/metacache-stream.go:936`** — potential data race on `w` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/metacache-stream.go:935`)
   - written inside the goroutine (`cmd/metacache-stream.go:950`)
   - written outside the goroutine (`cmd/metacache-stream.go:988`)

16. **RACE WARN** **`cmd/tables-catalog-scanner.go:3475`** — potential data race on `missing` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/tables-catalog-scanner.go:3468`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/tables-catalog-scanner.go:3462`)
   - written inside the goroutine (`cmd/tables-catalog-scanner.go:3475`)
   - the goroutine's writes appear serialized by a mutex (lock pairing not verified)

17. **RACE** **`cmd/delta-log-parser.go:378`** — potential data race on `readErr` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/delta-log-parser.go:373`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/delta-log-parser.go:370`)
   - written inside the goroutine (`cmd/delta-log-parser.go:394`)
   - read outside the goroutine (`cmd/delta-log-parser.go:404`)

18. **RACE** **`cmd/delta-log-parser.go:398`** — potential data race on `results` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/delta-log-parser.go:373`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/delta-log-parser.go:370`)
   - written inside the goroutine (`cmd/delta-log-parser.go:398`)
   - read outside the goroutine (`cmd/delta-log-parser.go:410`)

19. **RACE WARN** **`cmd/tables-stats.go:2069`** — potential data race on `results` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/tables-stats.go:2058`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/tables-stats.go:2056`)
   - written inside the goroutine (`cmd/tables-stats.go:2069`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

20. **RACE WARN** **`cmd/notification.go:673`** — potential data race on `distribErrs` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/notification.go:669`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/notification.go:664`)
   - written inside the goroutine (`cmd/notification.go:673`)
   - the goroutine's writes appear serialized by a mutex (lock pairing not verified)

21. **RACE WARN** **`cmd/notification.go:675`** — potential data race on `failedIndices` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/notification.go:669`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/notification.go:664`)
   - written inside the goroutine (`cmd/notification.go:675`)
   - the goroutine's writes appear serialized by a mutex (lock pairing not verified)

22. **RACE WARN** **`cmd/notification.go:1642`** — potential data race on `nodesOnlineIndex` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/notification.go:1640`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/notification.go:1635`)
   - written inside the goroutine (`cmd/notification.go:1642`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

23. **RACE** **`cmd/notification.go:1903`** — potential data race on `results` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/notification.go:1899`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/notification.go:1894`)
   - written inside the goroutine (`cmd/notification.go:1903`)
   - written outside the goroutine (`cmd/notification.go:1919`)

24. **RACE** **`cmd/notification.go:1948`** — potential data race on `results` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/notification.go:1944`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/notification.go:1939`)
   - written inside the goroutine (`cmd/notification.go:1948`)
   - written outside the goroutine (`cmd/notification.go:1969`)

25. **RACE WARN** **`cmd/notification.go:2039`** — potential data race on `lastDayStats` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/notification.go:2037`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/notification.go:2032`)
   - written inside the goroutine (`cmd/notification.go:2039`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

26. **RACE WARN** **`cmd/notification.go:2039`** — potential data race on `errs` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/notification.go:2037`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/notification.go:2032`)
   - written inside the goroutine (`cmd/notification.go:2039`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

27. **RACE WARN** **`cmd/perf-tests.go:117`** — potential data race on `objCountPerThread` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/perf-tests.go:114`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/perf-tests.go:113`)
   - written inside the goroutine (`cmd/perf-tests.go:154`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

28. **RACE WARN** **`cmd/perf-tests.go:146`** — potential data race on `retError` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/perf-tests.go:114`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/perf-tests.go:113`)
   - written inside the goroutine (`cmd/perf-tests.go:146`)
   - the goroutine's writes are inside sync.Once.Do — they execute at most once

29. **RACE WARN** **`cmd/perf-tests.go:156`** — potential data race on `uploadTimes` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/perf-tests.go:114`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/perf-tests.go:113`)
   - written inside the goroutine (`cmd/perf-tests.go:156`)
   - the goroutine's writes appear serialized by a mutex (lock pairing not verified)

30. **RACE WARN** **`cmd/perf-tests.go:218`** — potential data race on `retError` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/perf-tests.go:182`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/perf-tests.go:181`)
   - written inside the goroutine (`cmd/perf-tests.go:218`)
   - the goroutine's writes are inside sync.Once.Do — they execute at most once

31. **RACE WARN** **`cmd/perf-tests.go:243`** — potential data race on `downloadTimes` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/perf-tests.go:182`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/perf-tests.go:181`)
   - written inside the goroutine (`cmd/perf-tests.go:243`)
   - the goroutine's writes appear serialized by a mutex (lock pairing not verified)

32. **RACE WARN** **`cmd/perf-tests.go:244`** — potential data race on `downloadTTFB` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/perf-tests.go:182`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/perf-tests.go:181`)
   - written inside the goroutine (`cmd/perf-tests.go:244`)
   - the goroutine's writes appear serialized by a mutex (lock pairing not verified)

33. **RACE WARN** **`cmd/post-policy-fan-out.go:58`** — potential data race on `objInfos` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/post-policy-fan-out.go:55`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/post-policy-fan-out.go:53`)
   - written inside the goroutine (`cmd/post-policy-fan-out.go:58`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

34. **RACE WARN** **`cmd/post-policy-fan-out.go:69`** — potential data race on `errs` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/post-policy-fan-out.go:55`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/post-policy-fan-out.go:53`)
   - written inside the goroutine (`cmd/post-policy-fan-out.go:69`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

35. **RACE WARN** **`cmd/object-api-utils.go:1198`** — potential data race on `res` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/object-api-utils.go:1195`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/object-api-utils.go:1190`)
   - written inside the goroutine (`cmd/object-api-utils.go:1198`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

36. **RACE WARN** **`cmd/object-api-utils.go:1230`** — potential data race on `res` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/object-api-utils.go:1227`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/object-api-utils.go:1222`)
   - written inside the goroutine (`cmd/object-api-utils.go:1230`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

37. **RACE WARN** **`cmd/untar.go:250`** — potential data race on `asyncErr` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/untar.go:235`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/untar.go:169`)
   - written inside the goroutine (`cmd/untar.go:251`)
   - read outside the goroutine (`cmd/untar.go:172`)
   - the goroutine's writes appear serialized by a mutex (lock pairing not verified)
   - the branches guarding the launch and this access may be mutually exclusive — racy only if a code change lets them overlap

38. **RACE** **`cmd/object-multipart-handlers.go:1073`** — potential data race on `started` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/object-multipart-handlers.go:1064`)
   - written inside the goroutine (`cmd/object-multipart-handlers.go:1074`)
   - read outside the goroutine (`cmd/object-multipart-handlers.go:1102`)

39. **RACE WARN** **`cmd/restart.go:919`** — potential data race on `failedPeers` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/restart.go:902`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/restart.go:897`)
   - written inside the goroutine (`cmd/restart.go:919`)
   - the goroutine's writes appear serialized by a mutex (lock pairing not verified)

40. **RACE** **`cmd/delta-sharing-presign.go:212`** — potential data race on `opts` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/delta-sharing-presign.go:205`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/delta-sharing-presign.go:204`)
   - written inside the goroutine (`cmd/delta-sharing-presign.go:217`)

41. **RACE WARN** **`cmd/site-replication.go:3427`** — potential data race on `errs` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/site-replication.go:3422`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/site-replication.go:3421`)
   - written inside the goroutine (`cmd/site-replication.go:3427`)
   - read outside the goroutine (`cmd/site-replication.go:3465`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race
   - the branches guarding the launch and this access may be mutually exclusive — racy only if a code change lets them overlap

42. **RACE** **`cmd/site-replication.go:5683`** — potential data race on `errs` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/site-replication.go:5671`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/site-replication.go:5657`)
   - written inside the goroutine (`cmd/site-replication.go:5683`)

43. **RACE WARN** **`cmd/distributed-job.go:1097`** — potential data race on `state` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/distributed-job.go:1089`)
   - written outside the goroutine (`cmd/distributed-job.go:1112`)
   - the branches guarding the launch and this access may be mutually exclusive — racy only if a code change lets them overlap

44. **RACE** **`cmd/endpoint-ellipses.go:631`** — potential data race on `stripeSizes` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/endpoint-ellipses.go:629`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/endpoint-ellipses.go:622`)
   - written inside the goroutine (`cmd/endpoint-ellipses.go:631`)
   - read outside the goroutine (`cmd/endpoint-ellipses.go:642`)

45. **RACE WARN** **`cmd/erasure-common.go:46`** — potential data race on `newDisks` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/erasure-common.go:29`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-common.go:27`)
   - written inside the goroutine (`cmd/erasure-common.go:46`)
   - the goroutine's writes appear serialized by a mutex (lock pairing not verified)

46. **RACE** **`cmd/erasure-decode.go:190`** — potential data race on `p` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/erasure-decode.go:188`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-decode.go:177`)
   - written inside the goroutine (`cmd/erasure-decode.go:200`)
   - read outside the goroutine (`cmd/erasure-decode.go:178`)

47. **RACE WARN** **`cmd/erasure-decode.go:223`** — potential data race on `newBuf` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/erasure-decode.go:188`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-decode.go:177`)
   - written inside the goroutine (`cmd/erasure-decode.go:223`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

48. **RACE WARN** **`cmd/bootstrap-peer-server.go:376`** — potential data race on `offlineEndpoints` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/bootstrap-peer-server.go:371`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/bootstrap-peer-server.go:366`)
   - written inside the goroutine (`cmd/bootstrap-peer-server.go:376`)
   - the goroutine's writes appear serialized by a mutex (lock pairing not verified)

49. **RACE WARN** **`cmd/bootstrap-peer-server.go:390`** — potential data race on `incorrectConfigs` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/bootstrap-peer-server.go:371`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/bootstrap-peer-server.go:366`)
   - written inside the goroutine (`cmd/bootstrap-peer-server.go:390`)
   - the goroutine's writes appear serialized by a mutex (lock pairing not verified)

50. **RACE WARN** **`cmd/bootstrap-peer-server.go:391`** — potential data race on `configMismatches` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/bootstrap-peer-server.go:371`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/bootstrap-peer-server.go:366`)
   - written inside the goroutine (`cmd/bootstrap-peer-server.go:391`)
   - the goroutine's writes appear serialized by a mutex (lock pairing not verified)

51. **RACE** **`cmd/bootstrap-peer-server.go:400`** — potential data race on `verifiedPeers` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/bootstrap-peer-server.go:371`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/bootstrap-peer-server.go:366`)
   - written inside the goroutine (`cmd/bootstrap-peer-server.go:400`)
   - read outside the goroutine (`cmd/bootstrap-peer-server.go:367`)

52. **RACE WARN** **`cmd/bootstrap-peer-server.go:401`** — potential data race on `backendVersions` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/bootstrap-peer-server.go:371`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/bootstrap-peer-server.go:366`)
   - written inside the goroutine (`cmd/bootstrap-peer-server.go:401`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

53. **RACE WARN** **`cmd/bootstrap-peer-server.go:445`** — potential data race on `errs` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/bootstrap-peer-server.go:440`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/bootstrap-peer-server.go:439`)
   - written inside the goroutine (`cmd/bootstrap-peer-server.go:445`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

54. **RACE WARN** **`cmd/bootstrap-peer-server.go:448`** — potential data race on `results` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/bootstrap-peer-server.go:440`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/bootstrap-peer-server.go:439`)
   - written inside the goroutine (`cmd/bootstrap-peer-server.go:448`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

55. **RACE** **`cmd/bucket-replication.go:706`** — potential data race on `rinfos` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/bucket-replication.go:699`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/bucket-replication.go:658`)
   - written inside the goroutine (`cmd/bucket-replication.go:706`)

56. **RACE** **`cmd/bucket-replication.go:1566`** — potential data race on `rinfos` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/bucket-replication.go:1559`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/bucket-replication.go:1522`)
   - written inside the goroutine (`cmd/bucket-replication.go:1566`)

57. **RACE WARN** **`cmd/bucket-replication.go:3367`** — potential data race on `errs` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/bucket-replication.go:3340`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/bucket-replication.go:3329`)
   - written inside the goroutine (`cmd/bucket-replication.go:3367`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

58. **RACE WARN** **`cmd/bucket-replication.go:3433`** — potential data race on `errs` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/bucket-replication.go:3420`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/bucket-replication.go:3404`)
   - written inside the goroutine (`cmd/bucket-replication.go:3433`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

59. **RACE WARN** **`cmd/bucket-replication.go:3435`** — potential data race on `tagSlc` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/bucket-replication.go:3420`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/bucket-replication.go:3404`)
   - written inside the goroutine (`cmd/bucket-replication.go:3435`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

60. **RACE** **`cmd/bucket-replication.go:3827`** — potential data race on `workers` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/bucket-replication.go:3820`)
   - written outside the goroutine (`cmd/bucket-replication.go:3818`)

61. **RACE WARN** **`cmd/erasure-object.go:924`** — potential data race on `rawArr` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/erasure-object.go:898`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-object.go:888`)
   - written inside the goroutine (`cmd/erasure-object.go:924`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

62. **RACE WARN** **`cmd/erasure-object.go:925`** — potential data race on `metaArr` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/erasure-object.go:898`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-object.go:888`)
   - written inside the goroutine (`cmd/erasure-object.go:925`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

63. **RACE WARN** **`cmd/erasure-object.go:925`** — potential data race on `errs` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/erasure-object.go:898`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-object.go:888`)
   - written inside the goroutine (`cmd/erasure-object.go:925`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

64. **RACE WARN** **`cmd/erasure-object.go:2492`** — potential data race on `delObjErrs` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/erasure-object.go:2487`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-object.go:2485`)
   - written inside the goroutine (`cmd/erasure-object.go:2492`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

65. **RACE WARN** **`cmd/erasure-server-pool.go:977`** — potential data race on `poolObjInfos` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/erasure-server-pool.go:965`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-server-pool.go:963`)
   - written inside the goroutine (`cmd/erasure-server-pool.go:977`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

66. **RACE** **`cmd/erasure-server-pool.go:1277`** — potential data race on `results` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/erasure-server-pool.go:1269`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-server-pool.go:1266`)
   - written inside the goroutine (`cmd/erasure-server-pool.go:1277`)
   - read outside the goroutine (`cmd/erasure-server-pool.go:1319`)

67. **RACE** **`cmd/erasure-server-pool.go:1286`** — potential data race on `setStatus` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/erasure-server-pool.go:1269`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-server-pool.go:1266`)
   - written inside the goroutine (`cmd/erasure-server-pool.go:1286`)
   - read outside the goroutine (`cmd/erasure-server-pool.go:1320`)

68. **RACE WARN** **`cmd/erasure-server-pool.go:1287`** — potential data race on `firstErr` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/erasure-server-pool.go:1269`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-server-pool.go:1266`)
   - written inside the goroutine (`cmd/erasure-server-pool.go:1288`)
   - the goroutine's writes appear serialized by a mutex (lock pairing not verified)

69. **RACE WARN** **`cmd/erasure-server-pool.go:1654`** — potential data race on `results` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/erasure-server-pool.go:1652`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-server-pool.go:1650`)
   - written inside the goroutine (`cmd/erasure-server-pool.go:1654`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

70. **RACE WARN** **`cmd/erasure-server-pool.go:2196`** — potential data race on `dobjects` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/erasure-server-pool.go:2194`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-server-pool.go:2177`)
   - written inside the goroutine (`cmd/erasure-server-pool.go:2196`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

71. **RACE WARN** **`cmd/erasure-server-pool.go:2196`** — potential data race on `derrs` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/erasure-server-pool.go:2194`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-server-pool.go:2177`)
   - written inside the goroutine (`cmd/erasure-server-pool.go:2196`)
   - written outside the goroutine (`cmd/erasure-server-pool.go:2179`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race
   - the branches guarding the launch and this access may be mutually exclusive — racy only if a code change lets them overlap

72. **RACE** **`cmd/erasure-server-pool.go:3533`** — potential data race on `errs` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/erasure-server-pool.go:3530`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-server-pool.go:3525`)
   - written inside the goroutine (`cmd/erasure-server-pool.go:3533`)
   - read outside the goroutine (`cmd/erasure-server-pool.go:3537`)

73. **RACE WARN** **`cmd/erasure-server-pool.go:3606`** — potential data race on `errs` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/erasure-server-pool.go:3602`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-server-pool.go:3597`)
   - written inside the goroutine (`cmd/erasure-server-pool.go:3606`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

74. **RACE WARN** **`cmd/erasure-server-pool.go:3607`** — potential data race on `results` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/erasure-server-pool.go:3602`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-server-pool.go:3597`)
   - written inside the goroutine (`cmd/erasure-server-pool.go:3607`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

75. **RACE WARN** **`cmd/erasure-server-pool.go:4344`** — potential data race on `errs` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/erasure-server-pool.go:4340`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-server-pool.go:4335`)
   - written inside the goroutine (`cmd/erasure-server-pool.go:4344`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

76. **RACE WARN** **`cmd/erasure-sets.go:577`** — potential data race on `delErrs` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/erasure-sets.go:571`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-sets.go:570`)
   - written inside the goroutine (`cmd/erasure-sets.go:577`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

77. **RACE WARN** **`cmd/erasure-sets.go:578`** — potential data race on `delObjects` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/erasure-sets.go:571`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-sets.go:570`)
   - written inside the goroutine (`cmd/erasure-sets.go:578`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

78. **RACE WARN** **`cmd/erasure-sets.go:594`** — potential data race on `delErrs` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/erasure-sets.go:588`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-sets.go:587`)
   - written inside the goroutine (`cmd/erasure-sets.go:594`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

79. **RACE WARN** **`cmd/erasure-sets.go:595`** — potential data race on `delObjects` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/erasure-sets.go:588`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure-sets.go:587`)
   - written inside the goroutine (`cmd/erasure-sets.go:595`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

80. **RACE WARN** **`cmd/erasure.go:284`** — potential data race on `infos` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/erasure.go:279`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/erasure.go:277`)
   - written inside the goroutine (`cmd/erasure.go:284`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

81. **RACE** **`cmd/batch-jobs.go:1125`** — potential data race on `pool` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/batch-jobs.go:1125`)
   - written outside the goroutine (`cmd/batch-jobs.go:1124`)

82. **RACE WARN** **`cmd/namespace-lock.go:391`** — potential data race on `got` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/namespace-lock.go:388`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/namespace-lock.go:387`)
   - written inside the goroutine (`cmd/namespace-lock.go:391`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race

83. **RACE** **`cmd/metacache-server-pool.go:195`** — potential data race on `o` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/metacache-server-pool.go:194`)
   - written outside the goroutine (`cmd/metacache-server-pool.go:207`)

84. **RACE WARN** **`cmd/metacache-server-pool.go:338`** — potential data race on `allAtEOF` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/metacache-server-pool.go:332`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/metacache-server-pool.go:327`)
   - written inside the goroutine (`cmd/metacache-server-pool.go:338`)
   - the goroutine's writes appear serialized by a mutex (lock pairing not verified)

85. **RACE WARN** **`cmd/metacache-server-pool.go:340`** — potential data race on `errs` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/metacache-server-pool.go:332`)
   - launched inside a loop — multiple goroutine instances access the variable concurrently (`cmd/metacache-server-pool.go:327`)
   - written inside the goroutine (`cmd/metacache-server-pool.go:340`)
   - written outside the goroutine (`cmd/metacache-server-pool.go:342`)
   - index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race
   - both accesses appear guarded by the same mutex (lock pairing not verified)

86. **RACE WARN** **`cmd/metacache-server-pool.go:510`** — potential data race on `meta` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/metacache-server-pool.go:509`)
   - written inside the goroutine (`cmd/metacache-server-pool.go:510`)
   - read outside the goroutine (`cmd/metacache-server-pool.go:525`)
   - the branches guarding the launch and this access may be mutually exclusive — racy only if a code change lets them overlap

87. **RACE WARN** **`cmd/metacache-set.go:295`** — potential data race on `resErr` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/metacache-set.go:270`)
   - written inside the goroutine (`cmd/metacache-set.go:295`)
   - read outside the goroutine (`cmd/metacache-set.go:322`)
   - the branches guarding the launch and this access may be mutually exclusive — racy only if a code change lets them overlap

88. **RACE WARN** **`cmd/metacache-set.go:668`** — potential data race on `o` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/metacache-set.go:667`)
   - written outside the goroutine (`cmd/metacache-set.go:675`)
   - the branches guarding the launch and this access may be mutually exclusive — racy only if a code change lets them overlap

89. **RACE** **`cmd/metacache-set.go:668`** — potential data race on `partN` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/metacache-set.go:667`)
   - written outside the goroutine (`cmd/metacache-set.go:721`)

90. **RACE WARN** **`cmd/metacache-set.go:669`** — potential data race on `fi` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/metacache-set.go:667`)
   - written outside the goroutine (`cmd/metacache-set.go:651`)
   - the branches guarding the launch and this access may be mutually exclusive — racy only if a code change lets them overlap

91. **RACE WARN** **`cmd/metacache-set.go:669`** — potential data race on `metaArr` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/metacache-set.go:667`)
   - written outside the goroutine (`cmd/metacache-set.go:651`)
   - the branches guarding the launch and this access may be mutually exclusive — racy only if a code change lets them overlap

92. **RACE WARN** **`cmd/metacache-set.go:669`** — potential data race on `onlineDisks` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/metacache-set.go:667`)
   - written outside the goroutine (`cmd/metacache-set.go:651`)
   - the branches guarding the launch and this access may be mutually exclusive — racy only if a code change lets them overlap

93. **RACE WARN** **`cmd/metacache-set.go:971`** — potential data race on `mc` — captured by goroutine; theoretical in the current codebase
   - goroutine launched here (`cmd/metacache-set.go:959`)
   - written inside the goroutine (`cmd/metacache-set.go:982`)
   - read outside the goroutine (`cmd/metacache-set.go:1057`)
   - both accesses appear guarded by the same mutex (lock pairing not verified)

## writes to closed channels (25)

1. **CLOSED CHANNEL** **`internal/bpool/bpool_mmap.go:213`** — channel closed in bpool.(*BytePoolCapMmap).Close while 2 sender(s) elsewhere may still write to it
   - sender: bpool.(*BytePoolCapMmap).Populate (`internal/bpool/bpool_mmap.go:77`)
   - sender: bpool.(*BytePoolCapMmap).Get.func (`internal/bpool/bpool_mmap.go:149`)

2. **CLOSED CHANNEL** **`internal/jstream/decoder.go:151`** — channel closed in jstream.(*Decoder).decode while 3 sender(s) elsewhere may still write to it
   - sender: jstream.(*Decoder).emitAny (`internal/jstream/decoder.go:170`)
   - sender: jstream.(*Decoder).object (`internal/jstream/decoder.go:524`)
   - sender: jstream.(*Decoder).objectOrdered (`internal/jstream/decoder.go:612`)

3. **CLOSED CHANNEL** **`internal/jstream/scanner.go:39`** — channel closed in jstream.newScanner.func.func while 1 sender(s) elsewhere may still write to it
   - sender: jstream.newScanner.func (`internal/jstream/scanner.go:59`)

4. **CLOSED CHANNEL** **`internal/ioutil/ioutil.go:374`** — channel closed in ioutil.SafeClose while 75 sender(s) elsewhere may still write to it
   - sender: grid.(*Connection).WaitForConnect.func (`internal/grid/connection.go:563`)
   - sender: grid.(*StreamTypeHandler[Payload, Req, Resp]).register.func.func (`internal/grid/handlers.go:867`)
   - sender: grid.(*StreamTypeHandler[Payload, Req, Resp]).Call.func (`internal/grid/handlers.go:976`)
   - sender: grid.(*muxClient).RequestStateless (`internal/grid/muxclient.go:224`)
   - sender: grid.(*muxClient).RequestStateless (`internal/grid/muxclient.go:245`)
   - … and 70 more senders

5. **CLOSED CHANNEL** **`internal/logger/log-recorder-api.go:268`** — channel closed in logger.ReadAPILogsMemory.func while 3 sender(s) elsewhere may still write to it
   - sender: logger.ReadAPILogsMemory.func.func (`internal/logger/log-recorder-api.go:274`)
   - sender: logger.ReadAPILogsS3.func.func (`internal/logger/log-recorder-api.go:326`)
   - sender: logger.(*errorLogRecorder).read.func.func (`internal/logger/log-recorder-error.go:410`)

6. **CLOSED CHANNEL** **`internal/grid/fanout.go:204`** — channel closed in grid.(*Manager).FanOutCh while 1 sender(s) elsewhere may still write to it
   - sender: grid.(*Manager).FanOutCh.func (`internal/grid/fanout.go:237`)

7. **CLOSED CHANNEL** **`internal/alerts/recorder.go:597`** — channel closed in alerts.(*internalRecorder).read.func while 1 sender(s) elsewhere may still write to it
   - sender: alerts.(*internalRecorder).read.func.func (`internal/alerts/recorder.go:613`)

8. **CLOSED CHANNEL** **`internal/grid/stream.go:89`** — send on closed channel: `s`.`Requests` is closed earlier in this function
   - closed here (`internal/grid/stream.go:78`)

9. **CLOSED CHANNEL** **`internal/inventory/run-job.go:169`** — send on closed channel: `outObjResCh` is closed earlier in this function
   - closed here (`internal/inventory/run-job.go:162`)

10. **CLOSED CHANNEL** **`internal/inventory/run-job.go:183`** — send on closed channel: `outObjResCh` is closed earlier in this function
   - closed here (`internal/inventory/run-job.go:162`)

11. **CLOSED CHANNEL** **`internal/inventory/run-job.go:197`** — send on closed channel: `outObjResCh` is closed earlier in this function
   - closed here (`internal/inventory/run-job.go:162`)

12. **CLOSED CHANNEL** **`internal/inventory/run-job.go:212`** — send on closed channel: `outObjResCh` is closed earlier in this function
   - closed here (`internal/inventory/run-job.go:162`)

13. **CLOSED CHANNEL WARN** **`internal/inventory/run-job.go:792`** — send on closed channel: `resCh` is closed earlier in this function — theoretical in the current codebase
   - closed here (`internal/inventory/run-job.go:790`)
   - the close and this send are in different branches — they may be mutually exclusive

14. **CLOSED CHANNEL WARN** **`internal/inventory/run-job.go:808`** — send on closed channel: `resCh` is closed earlier in this function — theoretical in the current codebase
   - closed here (`internal/inventory/run-job.go:790`)
   - the close and this send are in different branches — they may be mutually exclusive

15. **CLOSED CHANNEL** **`cmd/batch-catalog.go:227`** — channel closed in cmd.(*BatchJobCatalogV2).doJob.func while 1 sender(s) elsewhere may still write to it
   - sender: cmd.(*BatchJobCatalogV2).doJob.func (`cmd/batch-catalog.go:220`)

16. **CLOSED CHANNEL** **`cmd/tables-compaction-distributed.go:275`** — channel closed in cmd.compactWarehouseDistributed.func while 1 sender(s) elsewhere may still write to it
   - sender: cmd.runPeerDispatcher (`cmd/tables-compaction-distributed.go:404`)

17. **CLOSED CHANNEL** **`cmd/background-newdisks-heal-ops.go:445`** — channel closed in cmd.(*healingTracker).stopHealErrsLogging while 1 sender(s) elsewhere may still write to it
   - sender: cmd.(*healingTracker).appendFailedHeal (`cmd/background-newdisks-heal-ops.go:467`)

18. **CLOSED CHANNEL** **`cmd/admin-handlers.go:2776`** — channel closed in cmd.(adminAPIHandlers).APILogsHandler.func while 2 sender(s) elsewhere may still write to it
   - sender: logger.encodeLogs (`internal/logger/log-recorder.go:310`)
   - sender: cmd.(*peerRESTClient).ReadAPILogs.func (`cmd/peer-rest-client.go:890`)

19. **CLOSED CHANNEL** **`cmd/admin-handlers.go:2844`** — channel closed in cmd.(adminAPIHandlers).APILogsHandler while 2 sender(s) elsewhere may still write to it
   - sender: logger.encodeAuditLogs (`internal/logger/log-recorder-audit.go:663`)
   - sender: alerts.(ReadResult).Encode (`internal/alerts/recorder.go:530`)

20. **CLOSED CHANNEL** **`cmd/tables-ui-list-cache.go:165`** — channel closed in cmd.computeTopTables while 1 sender(s) elsewhere may still write to it
   - sender: cmd.computeTopTables.func (`cmd/tables-ui-list-cache.go:152`)

21. **CLOSED CHANNEL** **`cmd/peer-rest-client.go:1131`** — channel closed in cmd.(*peerRESTClient).GetReplicationMRF.func.func while 1 sender(s) elsewhere may still write to it
   - sender: cmd.(*peerRESTClient).GetReplicationMRF.func (`cmd/peer-rest-client.go:1141`)

22. **CLOSED CHANNEL** **`cmd/erasure-healing.go:123`** — channel closed in cmd.(erasureObjects).listLowQuorumAndHeal while 1 sender(s) elsewhere may still write to it
   - sender: cmd.(erasureObjects).listLowQuorumAndHeal.func (`cmd/erasure-healing.go:160`)

23. **CLOSED CHANNEL** **`cmd/global-heal.go:237`** — channel closed in cmd.(*erasureObjects).healErasureSet.func while 1 sender(s) elsewhere may still write to it
   - sender: cmd.(*erasureObjects).healErasureSet.func (`cmd/global-heal.go:381`)

24. **CLOSED CHANNEL** **`cmd/batch-jobs.go:1602`** — channel closed in cmd.runBatchScan.func while 1 sender(s) elsewhere may still write to it
   - sender: cmd.runBatchScan.func (`cmd/batch-jobs.go:1600`)

25. **CLOSED CHANNEL** **`cmd/local-drive-mgr.go:130`** — channel closed in cmd.(*purgeWorker).Close.func while 1 sender(s) elsewhere may still write to it
   - sender: cmd.(*purgeWorker).Send (`cmd/local-drive-mgr.go:138`)

## unclosed file handles (0)

_none found_

## potential goroutine leaks (33)

1. **LEAK** **`internal/pubsub/pubsub.go:85`** — goroutine may block forever: `doneCh` has no writers or closers in the module
   - goroutine launched here (`internal/pubsub/pubsub.go:84`)

2. **LEAK** **`internal/ringbuffer/ring_buffer.go:97`** — goroutine may block forever: `ctx`.Done() has no writers or closers in the module
   - goroutine launched here (`internal/ringbuffer/ring_buffer.go:96`)

3. **LEAK** **`internal/logger/logonce.go:100`** — goroutine spins in an infinite loop with no return, break or channel wait
   - goroutine launched here (`internal/logger/logonce.go:123`)

4. **LEAK** **`internal/logger/logrotate.go:94`** — goroutine spins in an infinite loop with no return, break or channel wait
   - goroutine launched here (`internal/logger/logrotate.go:228`)

5. **LEAK** **`internal/kms/builtin.go:229`** — goroutine may block forever: time.After(10 * time.Second) has no writers or closers in the module
   - goroutine launched here (`internal/kms/builtin.go:227`)

6. **LEAK** **`internal/grid/handlers.go:970`** — goroutine may block forever: `reqT` has no writers or closers in the module
   - goroutine launched here (`internal/grid/handlers.go:968`)

7. **LEAK** **`internal/grid/connection.go:1147`** — goroutine may block forever: *`block` has no writers or closers in the module
   - goroutine launched here (`internal/grid/connection.go:1064`)

8. **LEAK** **`cmd/data-usage.go:37`** — goroutine may block forever: `dui` has no writers or closers in the module
   - goroutine launched here (`cmd/data-scanner.go:196`)

9. **LEAK** **`cmd/delta-sharing-auth.go:196`** — goroutine may block forever: `ticker`.`C` has no writers or closers in the module
   - goroutine launched here (`cmd/delta-sharing-auth.go:193`)

10. **LEAK** **`cmd/admin-handlers.go:1761`** — goroutine may block forever: `respCh` has no readers in the module
   - goroutine launched here (`cmd/admin-handlers.go:1758`)

11. **LEAK** **`cmd/admin-handlers.go:1771`** — goroutine may block forever: `respCh` has no readers in the module
   - goroutine launched here (`cmd/admin-handlers.go:1768`)

12. **LEAK** **`cmd/admin-handlers.go:1979`** — goroutine may block forever: `ctx`.Done() has no writers or closers in the module
   - goroutine launched here (`cmd/admin-handlers.go:1978`)

13. **LEAK** **`cmd/admin-handlers.go:2151`** — goroutine may block forever: `ctx`.Done() has no writers or closers in the module
   - goroutine launched here (`cmd/admin-handlers.go:2150`)

14. **LEAK** **`cmd/admin-handlers.go:2274`** — goroutine may block forever: `ctx`.Done() has no writers or closers in the module
   - goroutine launched here (`cmd/admin-handlers.go:2273`)

15. **LEAK** **`cmd/xl-storage-disk-id-check.go:1153`** — goroutine may block forever: `timeout`.`C` has no writers or closers in the module
   - goroutine launched here (`cmd/xl-storage-disk-id-check.go:1148`)

16. **LEAK** **`cmd/site-replication-utils.go:83`** — goroutine may block forever: `ctx`.Done() has no writers or closers in the module
   - goroutine launched here (`cmd/site-replication-utils.go:73`)

17. **LEAK** **`cmd/sftp-server-driver.go:392`** — goroutine may block forever: `clnt`.ListObjects(`cctx`, `bucket`, `opts`) has no writers or closers in the module
   - goroutine launched here (`cmd/sftp-server-driver.go:386`)

18. **LEAK** **`cmd/storage-rest-server.go:317`** — goroutine may block forever: `updates` has no writers or closers in the module
   - goroutine launched here (`cmd/storage-rest-server.go:315`)

19. **LEAK** **`cmd/storage-rest-server.go:320`** — goroutine may block forever: `out` has no readers in the module
   - goroutine launched here (`cmd/storage-rest-server.go:315`)

20. **LEAK** **`cmd/sftp-server.go:569`** — goroutine may block forever: `GlobalCordonContext`.Done() has no writers or closers in the module
   - goroutine launched here (`cmd/sftp-server.go:568`)

21. **LEAK** **`cmd/delta-snapshot-cache.go:236`** — goroutine may block forever: `ticker`.`C` has no writers or closers in the module
   - goroutine launched here (`cmd/delta-snapshot-cache.go:71`)

22. **LEAK** **`cmd/bucket-replication.go:4072`** — goroutine may block forever: `ctx`.Done() has no writers or closers in the module
   - goroutine launched here (`cmd/bucket-replication.go:4063`)

23. **LEAK** **`cmd/bucket-replication.go:4774`** — goroutine may block forever: `qTikr`.`C` has no writers or closers in the module
   - goroutine launched here (`cmd/bucket-replication.go:2714`)

24. **LEAK** **`cmd/bucket-targets.go:158`** — goroutine may block forever: `sys`.`hcClient`.Alive(`cctx`, madmin.AliveOpt… has no writers or closers in the module
   - goroutine launched here (`cmd/bucket-targets.go:603`)

25. **LEAK** **`cmd/erasure-server-pool-reload.go:76`** — goroutine may block forever: `sigChan` has no writers or closers in the module
   - goroutine launched here (`cmd/erasure-server-pool-reload.go:75`)

26. **LEAK** **`cmd/erasure.go:676`** — goroutine may block forever: `updates` has no writers or closers in the module
   - goroutine launched here (`cmd/erasure.go:674`)

27. **LEAK** **`cmd/iam-etcd-store.go:176`** — goroutine may block forever: `ch` has no readers in the module
   - goroutine launched here (`cmd/iam-etcd-store.go:145`)

28. **LEAK** **`cmd/iam-etcd-store.go:181`** — goroutine may block forever: `ch` has no readers in the module
   - goroutine launched here (`cmd/iam-etcd-store.go:145`)

29. **LEAK** **`cmd/ftp-server-driver.go:473`** — goroutine may block forever: `clnt`.ListObjects(`cctx`, `bucket`, `opts`) has no writers or closers in the module
   - goroutine launched here (`cmd/ftp-server-driver.go:467`)

30. **LEAK** **`cmd/ftp-server.go:182`** — goroutine may block forever: `GlobalCordonContext`.Done() has no writers or closers in the module
   - goroutine launched here (`cmd/ftp-server.go:181`)

31. **LEAK** **`cmd/batch-replicate.go:789`** — goroutine may block forever: `prefixObjInfoCh` has no writers or closers in the module
   - goroutine launched here (`cmd/batch-replicate.go:776`)

32. **LEAK** **`cmd/coalesced-lock.go:300`** — goroutine may block forever: `lockCtx`.`ctx`.Done() has no writers or closers in the module
   - goroutine launched here (`cmd/coalesced-lock.go:208`)

33. **LEAK** **`cmd/coalesced-lock.go:581`** — goroutine may block forever: `lockCtx`.`ctx`.Done() has no writers or closers in the module
   - goroutine launched here (`cmd/coalesced-lock.go:558`)
