// Comando demosite: sirve examples/demo por HTTP para probar el scraper sin
// depender de sitios de terceros.
package main

import (
	"flag"
	"log"
	"net/http"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8090", "dirección de escucha")
	dir := flag.String("dir", "examples/demo", "directorio a servir")
	flag.Parse()

	log.Printf("sitio de demostración en http://%s/ (sirviendo %s)", *addr, *dir)
	log.Fatal(http.ListenAndServe(*addr, http.FileServer(http.Dir(*dir))))
}
