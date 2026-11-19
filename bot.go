package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
)

// Inverte runas Unicode, sem separar os bytes de letras acentuadas.
func inverter(texto string) string {
	runas := []rune(texto)
	for i, j := 0, len(runas)-1; i < j; i, j = i+1, j-1 {
		runas[i], runas[j] = runas[j], runas[i]
	}
	return string(runas)
}

func main() {
	conn, err := net.Dial("tcp", "localhost:4444")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Bot não conseguiu conectar:", err)
		return
	}
	defer conn.Close()
	if _, err := fmt.Fprintln(conn, "JOIN\tBOT\tInversor"); err != nil {
		fmt.Fprintln(os.Stderr, "Falha ao identificar bot:", err)
		return
	}

	leitor := bufio.NewScanner(conn)
	leitor.Buffer(make([]byte, 4096), 64*1024)
	for leitor.Scan() {
		campos := strings.SplitN(leitor.Text(), "\t", 3)
		// O servidor envia PRIVATE apenas ao destinatário, com o remetente identificado.
		if len(campos) != 3 || campos[0] != "PRIVATE" {
			continue
		}
		remetente, texto := campos[1], campos[2]
		resposta := inverter(texto)
		fmt.Printf("Mensagem de: @%s\nRecebi: %s\nResposta: %s\n", remetente, texto, resposta)
		if _, err := fmt.Fprintf(conn, "PRIV\t@%s\t%s\n", remetente, resposta); err != nil {
			fmt.Fprintln(os.Stderr, "Falha ao responder:", err)
			return
		}
	}
	if err := leitor.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "Conexão do bot encerrada:", err)
	}
}
