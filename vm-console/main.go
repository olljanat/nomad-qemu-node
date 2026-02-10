package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/api/vms", vmsHandler)
	http.Handle("/novnc/", http.StripPrefix("/novnc/", http.FileServer(http.Dir("/usr/share/novnc"))))
	http.HandleFunc("/vnc/", vncHandler)

	log.Println("Server running on http://:5800")
	log.Fatal(http.ListenAndServe(":5800", nil))
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `
		<!DOCTYPE html>
		<html lang="en">
		<head>
		  <meta charset="UTF-8">
		  <meta name="viewport" content="width=device-width, initial-scale=1.0">
		  <title>VM List</title>
		</head>
		<body>
		  <h1>Virtual Machines</h1>
		  <ul id="vm-list"></ul>
		  <script>
			fetch('/api/vms')
			  .then(res => res.json())
			  .then(vms => {
				const ul = document.getElementById('vm-list');
				vms.forEach(vm => {
				  const li = document.createElement('li');
				  const a = document.createElement('a');
				  a.href = '#';
				  a.textContent = vm;
				  a.onclick = () => {
					window.open('/novnc/vnc.html?path=/vnc/' + vm + '&autoconnect=true&resize=scale');
				  };
				  li.appendChild(a);
				  ul.appendChild(li);
				});
			  })
			  .catch(err => console.error(err));
		  </script>
		</body>
		</html>
	`)
}

func vmsHandler(w http.ResponseWriter, r *http.Request) {
	files, err := os.ReadDir("/tmp/vnc/")
	if err != nil {
		http.Error(w, "Error reading directory", http.StatusInternalServerError)
		return
	}

	var vms []string
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".sock") {
			vms = append(vms, strings.TrimSuffix(file.Name(), ".sock"))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(vms)
}

func vncHandler(w http.ResponseWriter, r *http.Request) {
	vm := strings.TrimPrefix(r.URL.Path, "/vnc/")
	if vm == "" {
		http.Error(w, "Invalid VM", http.StatusBadRequest)
		return
	}

	sockPath := filepath.Join("/tmp/vnc/", vm+".sock")
	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		http.Error(w, "Socket not found", http.StatusNotFound)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}
	defer ws.Close()

	unixConn, err := net.Dial("unix", sockPath)
	if err != nil {
		log.Println("Unix dial error:", err)
		return
	}
	defer unixConn.Close()

	go func() {
		defer ws.Close()
		defer unixConn.Close()
		buf := make([]byte, 4096)
		for {
			n, err := unixConn.Read(buf)
			if err != nil {
				return
			}
			if err := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return
			}
		}
	}()

	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if _, err := unixConn.Write(msg); err != nil {
			return
		}
	}
}
