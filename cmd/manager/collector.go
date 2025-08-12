package main

import (
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/procfs"
	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
)

type ManagerCollector struct {
	LeaderChangeCnt       *prometheus.Desc
	SecsSinceLeaderChange *prometheus.Desc
	BytesIn               *prometheus.Desc
	BytesOut              *prometheus.Desc
	PacketsIn             *prometheus.Desc
	PacketsOut            *prometheus.Desc
	InterfaceState        *prometheus.Desc
	Namespace             string
}

func NewManagerCollector(network string, id int) *ManagerCollector {
	ns := "vlanman"
	mc := &ManagerCollector{
		Namespace: ns,
		LeaderChangeCnt: prometheus.NewDesc(
			ns+"_leader_change_cnt",
			"Number of times the leader election changed leaders",
			nil, prometheus.Labels{
				"network": network,
			},
		),
		SecsSinceLeaderChange: prometheus.NewDesc(
			ns+"_seconds_since_leader_change_cnt",
			"Number of seconds since the last leader change",
			nil, prometheus.Labels{
				"network": network,
			},
		),
		BytesIn: prometheus.NewDesc(
			ns+"_bytes_in",
			"Number of bytes received on the interface",
			[]string{"interface", "type", "id"}, nil,
		),
		BytesOut: prometheus.NewDesc(
			ns+"_bytes_out",
			"Number of bytes sent on the interface",
			[]string{"interface", "type", "id"}, nil,
		),
		PacketsIn: prometheus.NewDesc(
			ns+"_packets_in",
			"Number of packets received on the interface",
			[]string{"interface", "type", "id"}, nil,
		),
		PacketsOut: prometheus.NewDesc(
			ns+"_packets_out",
			"Number of packets sent on the interface",
			[]string{"interface", "type", "id"}, nil,
		),
		InterfaceState: prometheus.NewDesc(
			ns+"_interface_state",
			"State of the interface, value of 1 means 'up'",
			[]string{"interface", "state", "type", "id"}, nil,
		),
	}
	prometheus.MustRegister(mc)
	return mc
}

func (mc *ManagerCollector) Collect(ch chan<- prometheus.Metric) {
	cnt, secs := getValues()
	ch <- prometheus.MustNewConstMetric(
		mc.LeaderChangeCnt,
		prometheus.CounterValue,
		float64(cnt),
	)
	if secs != nil {
		ch <- prometheus.MustNewConstMetric(
			mc.SecsSinceLeaderChange,
			prometheus.GaugeValue,
			time.Since(*secs).Seconds(),
		)
	}
	fs, err := procfs.NewDefaultFS()
	if err != nil {
		klog.V(4).ErrorS(err, "Error creating a /proc fs reader")
		return
	}

	nd, err := fs.NetDev()
	if err != nil {
		klog.V(4).ErrorS(err, "Error reading /proc/net/dev")
		return
	}
	for _, dev := range nd {
		mc.collectDevInfo(dev, ch)
	}
}

func (mc *ManagerCollector) collectDevInfo(dev procfs.NetDevLine, ch chan<- prometheus.Metric) {
	vlanID := os.Getenv("VLAN_ID")
	link, err := netlink.LinkByName(dev.Name)
	if err != nil {
		klog.V(4).Infof("Error finding link '%s' by name: %s", dev.Name, err.Error())
		return
	}
	stateValue := 0.0
	if link.Attrs().OperState.String() == "up" {
		stateValue = 1.0
	}
	ch <- prometheus.MustNewConstMetric(
		mc.BytesIn,
		prometheus.GaugeValue,
		float64(dev.RxBytes),
		dev.Name, link.Type(), vlanID,
	)
	ch <- prometheus.MustNewConstMetric(
		mc.BytesOut,
		prometheus.GaugeValue,
		float64(dev.TxBytes),
		dev.Name, link.Type(), vlanID,
	)
	ch <- prometheus.MustNewConstMetric(
		mc.PacketsIn,
		prometheus.GaugeValue,
		float64(dev.RxPackets),
		dev.Name, link.Type(), vlanID,
	)
	ch <- prometheus.MustNewConstMetric(
		mc.PacketsOut,
		prometheus.GaugeValue,
		float64(dev.TxPackets),
		dev.Name, link.Type(), vlanID,
	)
	ch <- prometheus.MustNewConstMetric(
		mc.InterfaceState,
		prometheus.GaugeValue,
		stateValue,
		dev.Name, link.Attrs().OperState.String(), link.Type(), vlanID,
	)
}

func (mc *ManagerCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- mc.LeaderChangeCnt
	ch <- mc.SecsSinceLeaderChange
	ch <- mc.BytesIn
	ch <- mc.BytesOut
	ch <- mc.PacketsIn
	ch <- mc.PacketsOut
	ch <- mc.InterfaceState
}
