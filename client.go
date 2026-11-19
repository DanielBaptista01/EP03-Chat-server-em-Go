package main

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
)

func interpretar(comando string) (string, error) {
	linha := strings.TrimSpace(comando)
	switch {
	case linha == `\exit`:
		return "QUIT", nil
	case strings.HasPrefix(linha, `\changenick `):
		apelido := strings.TrimSpace(strings.TrimPrefix(linha, `\changenick `))
		if apelido == "" {
			return "", fmt.Errorf("informe o apelido: \\changenick NovoNome")
		}
		return "NICK\t" + apelido, nil
	case strings.HasPrefix(linha, `\msg `):
		mensagem := strings.TrimSpace(strings.TrimPrefix(linha, `\msg `))
		if mensagem == "" {
			return "", fmt.Errorf("a mensagem não pode ser vazia")
		}
		if strings.HasPrefix(mensagem, "@") {
			pos := strings.IndexAny(mensagem, " \t")
			if pos < 2 || strings.TrimSpace(mensagem[pos:]) == "" {
				return "", fmt.Errorf("use \\msg @Apelido mensagem")
			}
			return "PRIV\t" + mensagem[:pos] + "\t" + strings.TrimSpace(mensagem[pos:]), nil
		}
		return "PUB\t" + mensagem, nil
	default:
		return "", fmt.Errorf("comando desconhecido; use \\msg, \\changenick ou \\exit")
	}
}

func receber(conn net.Conn, terminou chan<- struct{}) {
	defer close(terminou)
	leitor := bufio.NewScanner(conn)
	leitor.Buffer(make([]byte, 4096), 64*1024)
	for leitor.Scan() {
		campos := strings.SplitN(leitor.Text(), "\t", 3)
		switch campos[0] {
		case "INFO":
			if len(campos) >= 2 {
				fmt.Println(campos[1])
			}
		case "PRIVATE":
			if len(campos) == 3 {
				fmt.Printf("@%s disse em privado: %s\n", campos[1], campos[2])
			}
		case "ERR":
			if len(campos) >= 2 {
				fmt.Fprintln(os.Stderr, "Erro:", campos[1])
			}
		}
	}
	if err := leitor.Err(); err != nil && !errors.Is(err, net.ErrClosed) {
		fmt.Fprintln(os.Stderr, "Conexão encerrada:", err)
	}
}

func main() {
	entrada := bufio.NewScanner(os.Stdin)
	entrada.Buffer(make([]byte, 4096), 64*1024)
	fmt.Print("Seu apelido: ")
	if !entrada.Scan() {
		return
	}
	apelido := strings.TrimSpace(entrada.Text())
	if apelido == "" {
		fmt.Fprintln(os.Stderr, "Erro: apelido vazio")
		return
	}

	conn, err := net.Dial("tcp", "localhost:4444")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Não foi possível conectar:", err)
		return
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "JOIN\tUSER\t%s\n", apelido); err != nil {
		fmt.Fprintln(os.Stderr, "Falha na identificação:", err)
		return
	}

	terminou := make(chan struct{})
	go receber(conn, terminou)
	fmt.Println("Comandos: \\msg texto | \\msg @apelido texto | \\changenick nome | \\exit")

	// Ler o teclado em outra gorrotina permite encerrar caso o servidor feche a conexão.
	comandos := make(chan string)
	go func() {
		defer close(comandos)
		for entrada.Scan() {
			comandos <- entrada.Text()
		}
	}()
	for {
		select {
		case <-terminou:
			return
		case linha, ok := <-comandos:
			if !ok {
				return
			}
			pacote, err := interpretar(linha)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Erro:", err)
				continue
			}
			if _, err := fmt.Fprintln(conn, pacote); err != nil {
				fmt.Fprintln(os.Stderr, "Falha ao enviar:", err)
				return
			}
			if pacote == "QUIT" {
				return
			}
		}
	}
}
