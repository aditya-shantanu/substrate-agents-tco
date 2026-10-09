cd /host/var/log/pods || exit 1
ls -d ate-system_atelet-* benchmark-workloads_benchmark-ateom-* 2>/dev/null | wc -l 1>&2
find ate-system_atelet-* benchmark-workloads_benchmark-ateom-* -type f \( -name '*.log' -o -name '*.gz' \) 2>/dev/null | while read f; do case $f in *.gz) zcat "$f";; *) cat "$f";; esac; done | grep -F 'timing breakdown' | grep -E '"time":"2026-10-09T0(6:[45]|7:[0-3])' | sed -E 's/^[0-9T:.Z+-]+ (stdout|stderr) F //'
