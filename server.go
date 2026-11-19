package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const endereco = "localhost:4444"
const limiteFila = 128

type participante struct {
	conn    net.Conn
	apelido string // Apenas a gorrotina gerenciadora altera este campo.
	tipo    string
	saida   chan string
}

type evento struct {
	acao     string
	quem     *participante
	alvo     string
	texto    string
	resposta chan error // Usado somente no cadastro inicial.
}

type sala struct {
	porApelido map[string]*participante
	ativos     map[*participante]bool
}

func chave(apelido string) string { return strings.ToLower(apelido) }

func apelidoValido(apelido string) bool {
	if apelido == "" || utf8.RuneCountInString(apelido) > 24 {
		return false
	}
	for _, r := range apelido {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

// O gerenciador é o único dono dos mapas e dos canais de saída: não há mutex nem send em canal fechado.
func (s *sala) enviar(p *participante, linha string) bool {
	select {
	case p.saida <- linha:
		return true
	default:
		// Nunca deixar um cliente lento bloquear os outros participantes.
		return false
	}
}

func (s *sala) anunciar(texto string) {
	log.Println(texto)
	var lentos []*participante
	for p := range s.ativos {
		if p.tipo == "USER" && !s.enviar(p, "INFO\t"+texto) {
			lentos = append(lentos, p)
		}
	}
	for _, p := range lentos {
		s.remover(p)
	}
}

func (s *sala) remover(p *participante) {
	if !s.ativos[p] {
		return
	}
	delete(s.ativos, p)
	delete(s.porApelido, chave(p.apelido))
	close(p.saida)
	classe := "Usuário"
	if p.tipo == "BOT" {
		classe = "Bot"
	}
	s.anunciar(fmt.Sprintf("%s @%s saiu", classe, p.apelido))
}

func (s *sala) erro(p *participante, mensagem string) {
	// Bots recebem apenas PRIVATE; respostas de controle são destinadas a usuários humanos.
	if p.tipo == "USER" && !s.enviar(p, "ERR\t"+mensagem) {
		s.remover(p)
	}
}

func (s *sala) processar(e evento) {
	p := e.quem
	if e.acao == "JOIN" {
		var err error
		switch {
		case p.tipo != "USER" && p.tipo != "BOT":
			err = fmt.Errorf("tipo de participante inválido")
		case !apelidoValido(p.apelido):
			err = fmt.Errorf("apelido inválido (use até 24 letras, números ou _)")
		case s.porApelido[chave(p.apelido)] != nil:
			err = fmt.Errorf("apelido já está em uso")
		}
		if err == nil {
			s.ativos[p] = true
			s.porApelido[chave(p.apelido)] = p
			classe := "Usuário"
			if p.tipo == "BOT" {
				classe = "Bot"
			}
			s.anunciar(fmt.Sprintf("%s @%s acabou de entrar", classe, p.apelido))
		}
		e.resposta <- err
		return
	}
	if !s.ativos[p] { // Comandos enfileirados após uma desconexão são ignorados.
		return
	}
	switch e.acao {
	case "PUB":
		if p.tipo != "USER" {
			return
		}
		if strings.TrimSpace(e.texto) == "" {
			s.erro(p, "mensagem vazia")
			return
		}
		s.anunciar(fmt.Sprintf("@%s disse: %s", p.apelido, e.texto))
	case "PRIV":
		if strings.TrimSpace(e.texto) == "" {
			s.erro(p, "mensagem vazia")
			return
		}
		alvo := s.porApelido[chave(strings.TrimPrefix(e.alvo, "@"))]
		if alvo == nil {
			s.erro(p, "destinatário não encontrado")
			return
		}
		log.Printf("@%s disse em privado: %s", p.apelido, e.texto)
		if !s.enviar(alvo, "PRIVATE\t"+p.apelido+"\t"+e.texto) {
			s.remover(alvo)
			s.erro(p, "destinatário desconectado")
		}
	case "NICK":
		if p.tipo != "USER" {
			return
		}
		if !apelidoValido(e.texto) {
			s.erro(p, "apelido inválido (use até 24 letras, números ou _)")
			return
		}
		ocupante := s.porApelido[chave(e.texto)]
		if ocupante != nil && ocupante != p {
			s.erro(p, "apelido já está em uso")
			return
		}
		antigo := p.apelido
		delete(s.porApelido, chave(antigo))
		p.apelido = e.texto
		s.porApelido[chave(p.apelido)] = p
		s.anunciar(fmt.Sprintf("Usuário @%s agora é @%s", antigo, p.apelido))
	case "QUIT":
		s.remover(p)
	default:
		s.erro(p, "comando desconhecido")
	}
}

func gerenciar(eventos <-chan evento) {
	s := &sala{porApelido: make(map[string]*participante), ativos: make(map[*participante]bool)}
	for e := range eventos {
		s.processar(e)
	}
}

// Cada conexão possui uma única gorrotina escritora; assim as linhas nunca se misturam.
func escrever(p *participante) {
	defer p.conn.Close()
	for linha := range p.saida {
		_ = p.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err := fmt.Fprintln(p.conn, linha); err != nil {
			return
		}
	}
}

func atender(conn net.Conn, eventos chan<- evento) {
	leitor := bufio.NewScanner(conn)
	leitor.Buffer(make([]byte, 4096), 64*1024)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if !leitor.Scan() {
		conn.Close()
		return
	}
	partes := strings.Split(leitor.Text(), "\t")
	if len(partes) != 3 || partes[0] != "JOIN" {
		fmt.Fprintln(conn, "ERR\tidentificação inválida: JOIN<TAB>USER|BOT<TAB>apelido")
		conn.Close()
		return
	}
	p := &participante{conn: conn, tipo: partes[1], apelido: partes[2], saida: make(chan string, limiteFila)}
	resposta := make(chan error, 1)
	eventos <- evento{acao: "JOIN", quem: p, resposta: resposta}
	if err := <-resposta; err != nil {
		fmt.Fprintln(conn, "ERR\t"+err.Error())
		conn.Close()
		return
	}
	go escrever(p)
	_ = conn.SetReadDeadline(time.Time{})
	for leitor.Scan() {
		linha := leitor.Text()
		campos := strings.SplitN(linha, "\t", 3)
		e := evento{quem: p, acao: campos[0]}
		switch campos[0] {
		case "PUB", "NICK":
			if len(campos) != 2 {
				e.acao = "INVALID"
			} else {
				e.texto = campos[1]
			}
		case "PRIV":
			if len(campos) != 3 || campos[1] == "" {
				e.acao = "INVALID"
			} else {
				e.alvo, e.texto = campos[1], campos[2]
			}
		case "QUIT":
			if len(campos) != 1 {
				e.acao = "INVALID"
			}
		default:
			e.acao = "INVALID"
		}
		eventos <- e
		if e.acao == "QUIT" {
			return
		}
	}
	if err := leitor.Err(); err != nil {
		log.Printf("Conexão encerrada: %v", err)
	}
	eventos <- evento{acao: "QUIT", quem: p}
}

func main() {
	log.SetFlags(0) // Mensagens exigidas pelo enunciado sem prefixo de data/hora.
	listener, err := net.Listen("tcp", endereco)
	if err != nil {
		log.Fatalf("Não foi possível abrir %s: %v", endereco, err)
	}
	defer listener.Close()
	log.Printf("Servidor escutando em %s", endereco)
	eventos := make(chan evento, 256)
	go gerenciar(eventos)
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Falha ao aceitar conexão: %v", err)
			continue
		}
		go atender(conn, eventos)
	}
}
