# Scan report — `/home/sveinn/code/ec-fork`

- scan /home/sveinn/code/ec-fork — 69 findings
- Generated at: 15:10:15
- Tool: code-analyzer

## potential data races (4)

1. **RACE** **`cmd/cp-main.go:340`** — potential data race on `errSeen` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/cp-main.go:324`)
   - written inside the goroutine (`cmd/cp-main.go:340`)
   - read outside the goroutine (`cmd/cp-main.go:508`)

2. **RACE** **`cmd/cp-main.go:347`** — potential data race on `totalObjects` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/cp-main.go:324`)
   - written inside the goroutine (`cmd/cp-main.go:347`)
   - read outside the goroutine (`cmd/cp-main.go:508`)

3. **RACE** **`cmd/support-diag-progress.go:143`** — potential data race on `err` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/support-diag-progress.go:141`)
   - written inside the goroutine (`cmd/support-diag-progress.go:143`)
   - read outside the goroutine (`cmd/support-diag-progress.go:169`)

4. **RACE** **`cmd/support-diag-progress.go:162`** — potential data race on `results` — captured by goroutine without synchronization
   - goroutine launched here (`cmd/support-diag-progress.go:141`)
   - written inside the goroutine (`cmd/support-diag-progress.go:162`)
   - read outside the goroutine (`cmd/support-diag-progress.go:167`)

## writes to closed channels (16)

1. **CLOSED CHANNEL** **`cmd/client-fs.go:175`** — channel closed in cmd.(*fsClient).Watch.func while 3 sender(s) elsewhere may still write to it
   - sender: cmd.(*fsClient).Watch.func (`cmd/client-fs.go:208`)
   - sender: cmd.(*fsClient).Watch.func (`cmd/client-fs.go:215`)
   - sender: cmd.(*fsClient).Watch.func (`cmd/client-fs.go:221`)

2. **CLOSED CHANNEL** **`cmd/client-fs.go:176`** — channel closed in cmd.(*fsClient).Watch.func while 1 sender(s) elsewhere may still write to it
   - sender: cmd.(*fsClient).Watch.func (`cmd/client-fs.go:201`)

3. **CLOSED CHANNEL WARN** **`cmd/client-fs.go:953`** — channel closed in cmd.(*fsClient).listInRoutine while 3 sender(s) elsewhere may still write to it — theoretical in the current codebase
   - sender: cmd.(*fsClient).listPrefixes (`cmd/client-fs.go:910`)
   - sender: cmd.(*fsClient).listPrefixes (`cmd/client-fs.go:929`)
   - sender: cmd.(*fsClient).listPrefixes (`cmd/client-fs.go:940`)
   - every sender is invoked only from the closing function's own call tree — likely sequential with the close, not concurrent

4. **CLOSED CHANNEL** **`cmd/client-fs.go:1028`** — channel closed in cmd.(*fsClient).listDirOpt while 6 sender(s) elsewhere may still write to it
   - sender: cmd.(*fsClient).listDirOpt.func (`cmd/client-fs.go:1043`)
   - sender: cmd.(*fsClient).listDirOpt.func (`cmd/client-fs.go:1051`)
   - sender: cmd.(*fsClient).listDirOpt.func (`cmd/client-fs.go:1059`)
   - sender: cmd.(*fsClient).listDirOpt.func (`cmd/client-fs.go:1074`)
   - sender: cmd.(*fsClient).listDirOpt.func (`cmd/client-fs.go:1080`)
   - … and 1 more senders

5. **CLOSED CHANNEL** **`cmd/client-fs.go:1107`** — channel closed in cmd.(*fsClient).listRecursiveInRoutine while 3 sender(s) elsewhere may still write to it
   - sender: cmd.(*fsClient).listRecursiveInRoutine.func (`cmd/client-fs.go:1157`)
   - sender: cmd.(*fsClient).listRecursiveInRoutine.func (`cmd/client-fs.go:1163`)
   - sender: cmd.(*fsClient).listRecursiveInRoutine.func (`cmd/client-fs.go:1178`)

6. **CLOSED CHANNEL** **`cmd/cp-main.go:360`** — channel closed in cmd.doCopySession.func.func while 2 sender(s) elsewhere may still write to it
   - sender: cmd.(*mirrorJob).startMirror (`cmd/mirror-main.go:825`)
   - sender: cmd.(*ParallelManager).addWorker.func (`cmd/parallel-manager.go:109`)

7. **CLOSED CHANNEL WARN** **`cmd/difference.go:412`** — channel closed in cmd.difference.func while 11 sender(s) elsewhere may still write to it — theoretical in the current codebase
   - sender: cmd.differenceInternal (`cmd/difference.go:268`)
   - sender: cmd.differenceInternal (`cmd/difference.go:283`)
   - sender: cmd.differenceInternal (`cmd/difference.go:307`)
   - sender: cmd.differenceInternal (`cmd/difference.go:313`)
   - sender: cmd.differenceInternal (`cmd/difference.go:325`)
   - … and 6 more senders
   - every sender is invoked only from the closing function's own call tree — likely sequential with the close, not concurrent

8. **CLOSED CHANNEL** **`cmd/mv-main.go:193`** — channel closed in cmd.(*removeManager).close while 1 sender(s) elsewhere may still write to it
   - sender: cmd.(*removeManager).add (`cmd/mv-main.go:188`)

9. **CLOSED CHANNEL WARN** **`cmd/client-s3.go:1879`** — channel closed in cmd.(*S3Client).listVersions.func while 3 sender(s) elsewhere may still write to it — theoretical in the current codebase
   - sender: cmd.(*S3Client).listVersionsRoutine (`cmd/client-s3.go:1892`)
   - sender: cmd.(*S3Client).listVersionsRoutine (`cmd/client-s3.go:1917`)
   - sender: cmd.(*S3Client).listVersionsRoutine (`cmd/client-s3.go:1939`)
   - every sender is invoked only from the closing function's own call tree — likely sequential with the close, not concurrent

10. **CLOSED CHANNEL WARN** **`cmd/client-s3.go:1966`** — channel closed in cmd.(*S3Client).List.func while 33 sender(s) elsewhere may still write to it — theoretical in the current codebase
   - sender: cmd.(*S3Client).versionedList (`cmd/client-s3.go:1997`)
   - sender: cmd.(*S3Client).versionedList (`cmd/client-s3.go:2011`)
   - sender: cmd.(*S3Client).versionedList (`cmd/client-s3.go:2022`)
   - sender: cmd.(*S3Client).versionedList (`cmd/client-s3.go:2031`)
   - sender: cmd.(*S3Client).versionedList (`cmd/client-s3.go:2039`)
   - … and 28 more senders
   - every sender is invoked only from the closing function's own call tree — likely sequential with the close, not concurrent

11. **CLOSED CHANNEL** **`cmd/parallel-manager.go:225`** — channel closed in cmd.(*ParallelManager).stopAndWait while 1 sender(s) elsewhere may still write to it
   - sender: cmd.(*ParallelManager).doQueueTask (`cmd/parallel-manager.go:220`)

12. **CLOSED CHANNEL WARN** **`cmd/rm-main.go:538`** — channel closed in cmd.listAndRemove while 1 sender(s) elsewhere may still write to it — theoretical in the current codebase
   - sender: cmd.purgeMarkedVersions (`cmd/rm-main.go:476`)
   - every sender is invoked only from the closing function's own call tree — likely sequential with the close, not concurrent

13. **CLOSED CHANNEL WARN** **`cmd/rm-main.go:587`** — send on closed channel: `contentCh` is closed earlier in this function — theoretical in the current codebase
   - closed here (`cmd/rm-main.go:538`)
   - the close and this send are in different branches — they may be mutually exclusive

14. **CLOSED CHANNEL WARN** **`cmd/rm-main.go:659`** — send on closed channel: `contentCh` is closed earlier in this function — theoretical in the current codebase
   - closed here (`cmd/rm-main.go:538`)
   - the close and this send are in different branches — they may be mutually exclusive

15. **CLOSED CHANNEL WARN** **`cmd/rm-main.go:729`** — send on closed channel: `contentCh` is closed earlier in this function — theoretical in the current codebase
   - closed here (`cmd/rm-main.go:538`)
   - the close and this send are in different branches — they may be mutually exclusive

16. **CLOSED CHANNEL** **`cmd/support-perf.go:538`** — channel closed in cmd.runPerfTests while 10 sender(s) elsewhere may still write to it
   - sender: cmd.mainAdminSpeedTestClientPerf.func (`cmd/support-perf-client.go:97`)
   - sender: cmd.mainAdminSpeedTestClientPerf.func (`cmd/support-perf-client.go:108`)
   - sender: cmd.mainAdminSpeedTestDrive.func (`cmd/support-perf-drive.go:108`)
   - sender: cmd.mainAdminSpeedTestDrive.func (`cmd/support-perf-drive.go:131`)
   - sender: cmd.mainAdminSpeedTestNetperf.func (`cmd/support-perf-net.go:98`)
   - … and 5 more senders

## unclosed file handles (7)

1. **FD LEAK** **`cmd/admin-cluster-info.go:97`** — file handle `f` ← os.Create(`fileName`) is never closed

2. **FD LEAK** **`cmd/admin-drive-info.go:120`** — file handle `f` ← os.Create(`fileName`) is never closed

3. **FD LEAK** **`cmd/telemetry-record.go:135`** — file handle `dst` ← os.Create(`out`) is never closed

4. **FD LEAK** **`cmd/admin-node-info.go:112`** — file handle `f` ← os.Create(`fileName`) is never closed

5. **FD LEAK** **`cmd/admin-policy-info.go:82`** — file handle `f` ← os.Create(`policyFile`) is never closed

6. **FD LEAK** **`cmd/admin-set-info.go:111`** — file handle `f` ← os.Create(`fileName`) is never closed

7. **FD LEAK** **`cmd/admin-pool-info.go:103`** — file handle `f` ← os.Create(`fileName`) is never closed

## potential goroutine leaks (42)

1. **LEAK WARN** **`cmd/client-fs.go:721`** — goroutine may block forever: `contentCh` has no writers or closers in the module
   - goroutine launched here (`cmd/client-fs.go:718`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

2. **LEAK WARN** **`cmd/client-fs.go:723`** — goroutine may block forever: `resultCh` has no readers in the module
   - goroutine launched here (`cmd/client-fs.go:718`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

3. **LEAK WARN** **`cmd/client-fs.go:737`** — goroutine may block forever: `resultCh` has no readers in the module
   - goroutine launched here (`cmd/client-fs.go:718`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

4. **LEAK WARN** **`cmd/client-fs.go:746`** — goroutine may block forever: `resultCh` has no readers in the module
   - goroutine launched here (`cmd/client-fs.go:718`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

5. **LEAK WARN** **`cmd/client-fs.go:752`** — goroutine may block forever: `resultCh` has no readers in the module
   - goroutine launched here (`cmd/client-fs.go:718`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

6. **LEAK WARN** **`cmd/client-fs.go:861`** — goroutine may block forever: `filteredCh` has no readers in the module
   - goroutine launched here (`cmd/client-fs.go:847`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link
   - the channel is buffered — the send only blocks once the buffer is full

7. **LEAK WARN** **`cmd/client-fs.go:968`** — goroutine may block forever: `contentCh` has no readers in the module
   - goroutine launched here (`cmd/client-fs.go:841`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

8. **LEAK WARN** **`cmd/client-fs.go:984`** — goroutine may block forever: `contentCh` has no readers in the module
   - goroutine launched here (`cmd/client-fs.go:841`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

9. **LEAK WARN** **`cmd/client-fs.go:1006`** — goroutine may block forever: `contentCh` has no readers in the module
   - goroutine launched here (`cmd/client-fs.go:841`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

10. **LEAK WARN** **`cmd/client-fs.go:1016`** — goroutine may block forever: `contentCh` has no readers in the module
   - goroutine launched here (`cmd/client-fs.go:841`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

11. **LEAK WARN** **`cmd/client-fs.go:1095`** — goroutine may block forever: `contentCh` has no readers in the module
   - goroutine launched here (`cmd/client-fs.go:838`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

12. **LEAK WARN** **`cmd/client-fs.go:1101`** — goroutine may block forever: `contentCh` has no readers in the module
   - goroutine launched here (`cmd/client-fs.go:838`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

13. **LEAK WARN** **`cmd/client-fs.go:1207`** — goroutine may block forever: `contentCh` has no readers in the module
   - goroutine launched here (`cmd/client-fs.go:836`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

14. **LEAK WARN** **`cmd/cp-url.go:229`** — goroutine may block forever: `copyURLsCh` has no readers in the module
   - goroutine launched here (`cmd/cp-url.go:223`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

15. **LEAK WARN** **`cmd/cp-url.go:242`** — goroutine may block forever: `copyURLsCh` has no readers in the module
   - goroutine launched here (`cmd/cp-url.go:223`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

16. **LEAK WARN** **`cmd/mv-main.go:154`** — goroutine may block forever: `resultCh` has no writers or closers in the module
   - goroutine launched here (`cmd/mv-main.go:152`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

17. **LEAK WARN** **`cmd/client-s3.go:1291`** — goroutine may block forever: `resultCh` has no readers in the module
   - goroutine launched here (`cmd/client-s3.go:1283`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

18. **LEAK WARN** **`cmd/client-s3.go:1296`** — goroutine may block forever: `resultCh` has no readers in the module
   - goroutine launched here (`cmd/client-s3.go:1283`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

19. **LEAK WARN** **`cmd/client-s3.go:1307`** — goroutine may block forever: `resultCh` has no readers in the module
   - goroutine launched here (`cmd/client-s3.go:1283`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

20. **LEAK WARN** **`cmd/client-s3.go:1319`** — goroutine may block forever: `resultCh` has no readers in the module
   - goroutine launched here (`cmd/client-s3.go:1283`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

21. **LEAK WARN** **`cmd/client-s3.go:1356`** — goroutine may block forever: `resultCh` has no readers in the module
   - goroutine launched here (`cmd/client-s3.go:1283`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

22. **LEAK WARN** **`cmd/client-s3.go:1361`** — goroutine may block forever: `resultCh` has no readers in the module
   - goroutine launched here (`cmd/client-s3.go:1283`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

23. **LEAK WARN** **`cmd/client-s3.go:1371`** — goroutine may block forever: `resultCh` has no readers in the module
   - goroutine launched here (`cmd/client-s3.go:1283`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

24. **LEAK WARN** **`cmd/client-s3.go:1402`** — goroutine may block forever: `resultCh` has no readers in the module
   - goroutine launched here (`cmd/client-s3.go:1283`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

25. **LEAK WARN** **`cmd/client-s3.go:1407`** — goroutine may block forever: `resultCh` has no readers in the module
   - goroutine launched here (`cmd/client-s3.go:1283`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

26. **LEAK WARN** **`cmd/client-s3.go:1442`** — goroutine may block forever: `resultCh` has no readers in the module
   - goroutine launched here (`cmd/client-s3.go:1283`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

27. **LEAK WARN** **`cmd/client-s3.go:1446`** — goroutine may block forever: `resultCh` has no readers in the module
   - goroutine launched here (`cmd/client-s3.go:1283`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

28. **LEAK WARN** **`cmd/client-s3.go:1456`** — goroutine may block forever: `resultCh` has no readers in the module
   - goroutine launched here (`cmd/client-s3.go:1283`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

29. **LEAK WARN** **`cmd/pipechan.go:62`** — goroutine may block forever: `currCh` has no readers in the module
   - goroutine launched here (`cmd/pipechan.go:41`)
   - the channel is buffered — the send only blocks once the buffer is full

30. **LEAK** **`cmd/pipechan.go:77`** — goroutine may block forever: `currCh` has no writers or closers in the module
   - goroutine launched here (`cmd/pipechan.go:71`)

31. **LEAK** **`cmd/support-perf-drive.go:114`** — goroutine may block forever: `resultCh` has no writers or closers in the module
   - goroutine launched here (`cmd/support-perf-drive.go:99`)

32. **LEAK** **`cmd/support-perf-object.go:141`** — goroutine may block forever: `resultCh` has no writers or closers in the module
   - goroutine launched here (`cmd/support-perf-object.go:126`)

33. **LEAK WARN** **`cmd/mirror-url.go:145`** — goroutine may block forever: `urlsCh` has no readers in the module
   - goroutine launched here (`cmd/mirror-url.go:282`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

34. **LEAK WARN** **`cmd/mirror-url.go:151`** — goroutine may block forever: `urlsCh` has no readers in the module
   - goroutine launched here (`cmd/mirror-url.go:282`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

35. **LEAK WARN** **`cmd/mirror-url.go:168`** — goroutine may block forever: `urlsCh` has no readers in the module
   - goroutine launched here (`cmd/mirror-url.go:282`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

36. **LEAK WARN** **`cmd/mirror-url.go:211`** — goroutine may block forever: `urlsCh` has no readers in the module
   - goroutine launched here (`cmd/mirror-url.go:282`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

37. **LEAK WARN** **`cmd/mirror-url.go:215`** — goroutine may block forever: `urlsCh` has no readers in the module
   - goroutine launched here (`cmd/mirror-url.go:282`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

38. **LEAK WARN** **`cmd/mirror-url.go:227`** — goroutine may block forever: `urlsCh` has no readers in the module
   - goroutine launched here (`cmd/mirror-url.go:282`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

39. **LEAK WARN** **`cmd/mirror-url.go:239`** — goroutine may block forever: `urlsCh` has no readers in the module
   - goroutine launched here (`cmd/mirror-url.go:282`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

40. **LEAK WARN** **`cmd/mirror-url.go:249`** — goroutine may block forever: `urlsCh` has no readers in the module
   - goroutine launched here (`cmd/mirror-url.go:282`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

41. **LEAK WARN** **`cmd/mirror-url.go:254`** — goroutine may block forever: `urlsCh` has no readers in the module
   - goroutine launched here (`cmd/mirror-url.go:282`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link

42. **LEAK WARN** **`cmd/admin-update.go:410`** — goroutine may block forever: `ch` has no writers or closers in the module
   - goroutine launched here (`cmd/admin-update.go:394`)
   - the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link
