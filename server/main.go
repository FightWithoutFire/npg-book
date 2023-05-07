package main

import (
	"bytes"
	"context"
	"github.com/gin-gonic/gin"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/irai/arp"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func main() {

	//localIP := net.ParseIP("127.0.0.1")
	//remoteIP := net.ParseIP("2607:f8b0:4002:c06::65")
	//
	//fmt.Println("local IP: ", localIP)
	//fmt.Println("remote IP: ", remoteIP)
	//
	//host, p, err := net.SplitHostPort("127.0.0.1:80")
	//if err != nil {
	//	return
	//}
	//fmt.Println(host)
	//fmt.Println(p)
	//num := 6
	//for index := 0; index < num; index++ {
	//	resp, _ := http.Get("https://www.baidu.com")
	//	_, _ = ioutil.ReadAll(resp.Body)
	//}
	//fmt.Printf("此时goroutine个数= %d\n", runtime.NumGoroutine())
	//
	////
	//route := gin.Default()
	//route.POST("/testing", startPage)
	//route.Run(":8085")
	// allow non root user to execute by compare with euid

}

func IPToUInt32(ipnr net.IP) uint32 {
	bits := strings.Split(ipnr.String(), ".")

	b0, _ := strconv.Atoi(bits[0])
	b1, _ := strconv.Atoi(bits[1])
	b2, _ := strconv.Atoi(bits[2])
	b3, _ := strconv.Atoi(bits[3])

	var sum uint32

	sum += uint32(b0) << 24
	sum += uint32(b1) << 16
	sum += uint32(b2) << 8
	sum += uint32(b3)

	return sum
}

type Person struct {
	JoiningDate time.Time `json:"joining_date" form:"joining_date" time_format:"2006-01-02" time_utc:"1"`
	FeeAmount   uint64    `json:"fee_amount" form:"fee_amount"`
}

func startPage(c *gin.Context) {
	var person Person
	// If `GET`, only `Form` binding engine (`query`) used.
	// If `POST`, first checks the `content-type` for `JSON` or `XML`, then uses `Form` (`form-data`).
	// See more at https://github.com/gin-gonic/gin/blob/master/binding/binding.go#L48
	err := c.ShouldBind(&person)
	if err == nil {
		log.Println(person.JoiningDate)
		log.Println(person.FeeAmount)

	} else {
		log.Println(err)
	}

	c.String(200, "Success")
}

func GetMAC() {

	HomeRouterIP := net.ParseIP("192.168.0.1").To4()
	HomeLAN := net.IPNet{IP: net.ParseIP("192.168.0.0").To4(), Mask: net.CIDRMask(25, 32)}
	NIC := "eth0"
	HostMAC, _ := net.ParseMAC("xx:xx:xx:xx:xx:xx")
	HostIP := net.ParseIP("192.168.1.2").To4()

	c, err := arp.New(arp.Config{
		NIC:                     NIC,
		HostMAC:                 HostMAC,
		HostIP:                  HostIP,
		RouterIP:                HomeRouterIP,
		HomeLAN:                 HomeLAN,
		ProbeInterval:           time.Minute,
		FullNetworkScanInterval: 0,
	})
	if err != nil {
		log.Fatal("error ", err)
	}
	defer c.Close()

	go c.ListenAndServe(context.Background())

	c.PrintTable()

	arpChannel := make(chan arp.MACEntry, 16)
	c.AddNotificationChannel(arpChannel)

	go arpNotification(arpChannel)
}

func arpNotification(arpChannel chan arp.MACEntry) {
	for {
		select {
		case MACEntry := <-arpChannel:
			log.Printf("notification got ARP MACEntry for %s", MACEntry.MAC)

		}
	}

	client := &http.Client{}

	client.Do(&http.Request{
		Method: "GET",
		URL: &url.URL{
			Scheme: "http",
			Host:   "",
		},
	})
}

type IP uint32

// 将 IP(uint32) 转换成 可读性IP字符串
func (ip IP) String() string {
	var bf bytes.Buffer
	for i := 1; i <= 4; i++ {
		bf.WriteString(strconv.Itoa(int((ip >> ((4 - uint(i)) * 8)) & 0xff)))
		if i != 4 {
			bf.WriteByte('.')
		}
	}
	return bf.String()
}

// ipNet 存放 IP地址和子网掩码
var ipNet *net.IPNet

var localHaddr net.HardwareAddr

var iface string

// 发送arp包
// ip 目标IP地址
func sendArpPackage(ip IP) {
	srcIp := net.ParseIP(ipNet.IP.String()).To4()
	dstIp := net.ParseIP(ip.String()).To4()
	if srcIp == nil || dstIp == nil {
		log.Fatal("ip 解析出问题")
	}
	// 以太网首部
	// EthernetType 0x0806  ARP
	ether := &layers.Ethernet{
		SrcMAC:       localHaddr,
		DstMAC:       net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		EthernetType: layers.EthernetTypeARP,
	}

	a := &layers.ARP{
		AddrType:          layers.LinkTypeEthernet,
		Protocol:          layers.EthernetTypeIPv4,
		HwAddressSize:     uint8(6),
		ProtAddressSize:   uint8(4),
		Operation:         uint16(1), // 0x0001 arp request 0x0002 arp response
		SourceHwAddress:   localHaddr,
		SourceProtAddress: srcIp,
		DstHwAddress:      net.HardwareAddr{0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		DstProtAddress:    dstIp,
	}

	buffer := gopacket.NewSerializeBuffer()
	var opt gopacket.SerializeOptions
	gopacket.SerializeLayers(buffer, opt, ether, a)
	outgoingPacket := buffer.Bytes()

	handle, err := pcap.OpenLive(iface, 2048, false, 30*time.Second)
	if err != nil {
		log.Fatal("pcap打开失败:", err)
	}
	defer handle.Close()

	err = handle.WritePacketData(outgoingPacket)
	if err != nil {
		log.Fatal("发送arp数据包失败..")
	}
}

func setupNetInfo(f string) {
	var ifs []net.Interface
	var err error
	if f == "" {
		ifs, err = net.Interfaces()
	} else {
		// 已经选择iface
		var it *net.Interface
		it, err = net.InterfaceByName(f)
		if err == nil {
			ifs = append(ifs, *it)
		}
	}
	if err != nil {
		log.Fatal("无法获取本地网络信息:", err)
	}
	for _, it := range ifs {
		addr, _ := it.Addrs()
		for _, a := range addr {
			if ip, ok := a.(*net.IPNet); ok && !ip.IP.IsLoopback() {
				if ip.IP.To4() != nil {
					ipNet = ip
					localHaddr = it.HardwareAddr
					iface = it.Name
					goto END
				}
			}
		}
	}
END:
	if ipNet == nil || len(localHaddr) == 0 {
		log.Fatal("无法获取本地网络信息")
	}
}
