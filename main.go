package main


import "C"
import (
	"database/sql"
	set "github.com/deckarep/golang-set"
	_ "github.com/mattn/go-sqlite3"
	"net"
	"strconv"
	"sync"
	"time"
)

type Protocol int

const (
	// TCP is tcp protocol
	TCP Protocol = iota
	// ICMP is ICMP protocol
	ICMP
)

var IPSet = set.NewSet()

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	//_ping(int(ICMP), "SELECT ip,tcping_port,enable_tcping_test FROM data_centers Where country='日本' and idc='Vultr'", 900, 1)
}

//export ping
func ping(pingType int, sqlQuery *C.char, pingCount int, timeout int) {
	sqlQueryString := C.GoString(sqlQuery)
	switch Protocol(pingType) {
	case ICMP:
		icmping(sqlQueryString, pingCount, time.Second*time.Duration(timeout))
	case TCP:
		tcping(sqlQueryString, pingCount, time.Second*time.Duration(timeout))
	}
}

func _ping(pingType int, sqlQuery string, pingCount int, timeout int) {
	switch Protocol(pingType) {
	case ICMP:
		icmping(sqlQuery, pingCount, time.Second*time.Duration(timeout))
	case TCP:
		tcping(sqlQuery, pingCount, time.Second*time.Duration(timeout))
	}
}

func icmping(sqlQuery string, pingCount int, timeout time.Duration) {
	db, err := sql.Open("sqlite3", "./DataCenter.db")
	checkErr(err)
	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	rows, err := db.Query(sqlQuery)
	defer db.Close()
	checkErr(err)

	var wg sync.WaitGroup

	for rows.Next() {
		var ip string
		var tcpingPort int
		var enableTcping int
		err = rows.Scan(&ip,&tcpingPort,&enableTcping)
		checkErr(err)

		if !IPSet.Contains(ip) {
			IPSet.Add(ip)
			wg.Add(1)
			go imcpingHandler(ip, pingCount, timeout, &wg, db)
		}
	}
	wg.Wait()
}

func imcpingHandler(host string, pingCount int, timeout time.Duration, wg *sync.WaitGroup, db *sql.DB) {
	defer wg.Done()

	var seq int16 = 1
	id0, id1 := genidentifier()
	const EchoRequestHeadLen = 8
	const size = 0

	sendN := 0
	recvN := 0
	lostN := 0
	sumT := 0.0

	for pingCount > 0 {
		sendN++
		var msg = make([]byte, size+EchoRequestHeadLen)
		msg[0] = 8                // echo
		msg[1] = 0                // code 0
		msg[2] = 0                // checksum
		msg[3] = 0                // checksum
		msg[4], msg[5] = id0, id1 //identifier[0] identifier[1]
		msg[6], msg[7] = id0, id1 //sequence[0], sequence[1]

		length := size + EchoRequestHeadLen

		check := checkSum(msg[0:length])
		msg[2] = byte(check >> 8)
		msg[3] = byte(check & 255)

		conn, err := net.DialTimeout("ip:icmp", host, timeout)

		checkErr(err)

		startTime := time.Now()
		_ = conn.SetDeadline(startTime.Add(timeout))
		_, err = conn.Write(msg[0:length])

		const EchoReplyHeadLen = 20

		var receive = make([]byte, EchoReplyHeadLen+length)
		n, err := conn.Read(receive)
		_ = n

		var endDuration = float64(time.Since(startTime).Microseconds()) / 1000.0

		if err != nil || receive[EchoReplyHeadLen+4] != msg[4] || receive[EchoReplyHeadLen+5] != msg[5] || receive[EchoReplyHeadLen+6] != msg[6] || receive[EchoReplyHeadLen+7] != msg[7] || endDuration >= float64(timeout.Milliseconds()) || receive[EchoReplyHeadLen] == 11 {
			lostN++
		} else {
			recvN++
			sumT += endDuration
			//ttl := int(receive[8])
		}
		seq++
		pingCount--
	}
	stat(db, host, lostN, recvN, sumT)
}

func checkSum(msg []byte) uint16 {
	sum := 0

	length := len(msg)
	for i := 0; i < length-1; i += 2 {
		sum += int(msg[i])*256 + int(msg[i+1])
	}
	if length%2 == 1 {
		sum += int(msg[length-1]) * 256
	}

	sum = (sum >> 16) + (sum & 0xffff)
	sum += sum >> 16
	var answer = uint16(^sum)
	return answer

}

func tcping(sqlQuery string, pingCount int, timeout time.Duration) {
	db, err := sql.Open("sqlite3", "./DataCenter.db")
	checkErr(err)
	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	rows, err := db.Query(sqlQuery)
	defer db.Close()
	checkErr(err)

	var wg sync.WaitGroup

	for rows.Next() {
		var ip string
		var tcpingPort int
		var enableTcping int
		err = rows.Scan(&ip,&tcpingPort,&enableTcping)
		checkErr(err)

		if !IPSet.Contains(ip) && enableTcping==1 {
			IPSet.Add(ip)
			wg.Add(1)
			go tcpingHandler(ip, pingCount, tcpingPort, timeout, &wg, db)
		}
	}
	wg.Wait()
}

func tcpingHandler(ip string, count int, port int, timeout time.Duration, s *sync.WaitGroup, db *sql.DB) {
	defer s.Done()
	sendN := 0
	recvN := 0
	lostN := 0
	sumT := 0.0
	for count >0{
		count--
		sendN++
		startTime := time.Now()
		conn, err := net.DialTimeout("tcp",ip+":"+strconv.Itoa(port),timeout)
		if err != nil {
			lostN++
		}else {
			recvN++
			//_ = conn.SetDeadline(startTime.Add(timeout))
			var endDuration = float64(time.Since(startTime).Microseconds()) / 1000.0
			sumT+=endDuration
			_ = conn.Close()
		}
	}
	stat(db,ip,lostN,recvN,sumT)
}

var mu sync.Mutex
var idf int16 = 0

func genidentifier() (byte, byte) {
	mu.Lock()
	defer mu.Unlock()
	idf++
	return byte(idf / 256), byte(idf % 256)
}

func stat(db *sql.DB, ip string, lostN int, recvN int, sumT float64) {
	rows, err := db.Query("SELECT ping_loss,ping_received,ping_time FROM data_centers WHERE ip='" + ip + "'")
	checkErr(err)
	defer rows.Close()
	for rows.Next() {
		var pingLoss int
		var pingReceived int
		var pingTime float64
		err = rows.Scan(&pingLoss, &pingReceived, &pingTime)
		checkErr(err)
		pingTime *= float64(pingReceived)
		pingLoss += lostN
		pingReceived += recvN
		pingTime += sumT
		if pingReceived == 0 {
			pingTime = 0
		} else {
			pingTime /= float64(pingReceived)
		}
		stmt, err := db.Prepare("UPDATE data_centers set ping_loss=?,ping_received=?,ping_time=? where ip=?")
		checkErr(err)
		_, err = stmt.Exec(pingLoss, pingReceived, pingTime, ip)
		checkErr(err)
	}
}
