#!/bin/bash

## Source https://cdrdv2-public.intel.com/686407/kvm-tuning-guide-icx.pdf
# 3.2.3. Enable Huge Page
echo always > /sys/kernel/mm/transparent_hugepage/enabled

# 3.2.2. Processor Frequency
for gov in /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor; do
    if [ -f "$gov" ]; then
        echo performance > "$gov"
    fi
done
