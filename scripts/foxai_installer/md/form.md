# FoxAI Installer Product Form

## 1. Product Goal

1. Installer name: Foxai_Datalakehouse
2. Final delivery format: i dont know, binary file like we did, compiled from go
3. Target OS/arch: linux server
4. Main goal in one sentence: setup and config a fresh server to be like our current working cluster, so the customer only need to write their dag and task scripts to work, also if ran on a working cluster, broken, or more, it can handle every use case and test case.
5. Should one binary handle both fresh install and existing-cluster repair? `yes/no` yes

## 2. Modes

6. Which modes do you want?
   - `preflight`
   - `install`
   - `repair`
   - `reconcile/upgrade`
   - `recommend-only`
   - other:
     i want every use and test case - modes
7. For each mode, should it be:
   - read-only
   - safe-auto-fix
   - ask-before-change
     it should do all 3
8. Should full `install` be blocked on an already-installed cluster by default? `yes/no` yes, but it should define "already-installed".
9. Should `repair` be allowed to change existing configs automatically? `yes/no` yes, but it should tell the user what configs got changed
10. Should `reconcile` be allowed to overwrite drifted FoxAI-managed files? `yes/no` yes, but it should tell the user what files got overwrited

## 3. Ownership

11. Which paths does FoxAI fully own?
12. Which paths may FoxAI edit only inside managed blocks?
13. Which paths must FoxAI never touch automatically?
14. Does FoxAI own `/opt/spark`? `yes/no/depends`
15. Does FoxAI own `/home/ubuntu/hadoop`? `yes/no/depends`
16. Does FoxAI own `/etc/hosts` full file or only a FoxAI block?
17. Does FoxAI own user `.bashrc` fully or only append/update a managed block?

i dont understand this ownership thing. We protect how we connect spark, hadoop,... together, how we configure things, we dont protect open-source.

## 4. Existing Cluster Rules

18. If cluster is already healthy, should installer:

- skip everything
- verify and print status only
- repair small drift
  yeah

19. If a config differs from desired FoxAI value, should installer:

- warn only
- block
- auto-fix
  if the cluster is partially installed, before fix and do anything, report to user and get confirm first

20. If a value differs but cluster is working, do you want:

- preserve live cluster
- enforce desired config
- ask case by case
  tell the user what differs and get confirm if the user want to fix or not

21. Example: if `dfs.replication` is different from expected but valid, should installer:

- preserve
- change
- warn only
  prompt the user for the choices

22. If DataNode env lines are missing but cluster works, should installer:

- add them
- warn only
- ignore

For every case that the partially installed cluster happen, after check everything, just tell the user what is different, then prompt the user to select things to fix, or select all

## 5. Fresh Cluster Rules

23. Fresh install should assume which Linux distro/version? if the distro/version is good enough, then install like normal, if the distro/version is not compatible, prompt the user to exit or still install.
24. Should installer require user `ubuntu`, or support custom users? definitely custom user
25. Should package versions stay pinned exactly as today? `yes/no` the package like spark, hadoop,... should stay exactly as today.
26. Should Kakao mirror remain optional? `yes/no` optional, but at least asked the user if they choose, do they know their server need to use kakao. If they dont know, just go with the kakao.
27. Should installer automatically set up SSH keys and passwordless sudo? `yes/no` yes always

## 6. Safety

28. What actions must always require explicit confirmation? i told you above, a partially installed cluster must always as the user to confirm what gonna get fixed/patched/install/...
29. Should NameNode format ever run automatically? `yes/no` hdfs format or our stuff's mandatory format run are always run automatically
30. If yes, under what exact conditions? i told you above. If your wording meant format as wipe a whole VM, then no, ask the user first
31. Should `rsync --delete` ever be used automatically? `yes/no` yes, if you confirmed the conditions for it is matched, if not warn the user, prompt for choice
32. If yes, only on:
33. If installer detects ambiguous drift, should it:

- block
- continue read-only
- offer choices
  always offer choices. If the user confirmed our flow then proceed like normal, if the user did not, skip that drift and move on

## 7. Cluster Discovery

34. Which signals should define “already installed”? check the namenode + datanode of the user cluster to see if everything is there or not.
35. Should installer write its own state file/manifest? `yes/no` yes, it can do whatever it want to follow the purpose
36. If yes, where should that live? you decide what is the most logical place.
37. What should that manifest contain? you decide

## 8. Verification

38. What checks must pass before installer says success? after install, it should check if yarn and dfs can start, and namenode + datanode's jps command show the correct things
39. Should it verify:

- Hadoop config files
- Java/Hadoop/Spark binaries
- SSH to all nodes
- sudo on all nodes
- HDFS health
- YARN node list
- other:
  you decide

40. Should installer print a final summary table? `yes/no` yes, always, print the state, what is already good and what not

## 9. UI / Logs

41. Should terminal UI be:

- simple text
- step-by-step sections
- colorized status
  yeah, dont make it too complicated, UI simple but have progress bar or sth.

42. Do you want:

- `SKIP`
- `OK`
- `WARN`
- `DRIFT`
- `FIXED`
- `BLOCKED`
  shown explicitly?
  of course the user should see these logs to understand, no?

43. Should logs also be saved to a file? `yes/no`
    yeah, you decide, but same as the manifest
44. If yes, where?

## 10. Customer Behavior

45. Should customers be allowed to re-run installer safely many times? `yes/no` yes, 100%
46. Should customer see exact config diffs before changes? `yes/no` yes 100%
47. Should customer be able to choose nodes to repair individually? `yes/no` yes 100%
48. Should installer support cluster expansion later? `yes/no` yes,
49. If yes, how should expansion work in your ideal behavior? like this: 1st run 5 datanodes for example, done. 2nd run the user input 3 datanodes and 3 new ip address, install like normal. The configs should be append, not replace right? so it should not be complicated

## 11. Non-Negotiables

50. List your hard rules: flexible
51. List anything installer must never do: never fix patch anything without confirm
52. List anything installer must always do: always follow the version, configs,... from our defined
