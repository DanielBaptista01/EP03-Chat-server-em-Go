# EP03 — Chat server em Go

Implementação da EP03 de **Paradigmas de Programação — Centro Universitário Senac**. O projeto demonstra comunicação TCP cliente-servidor e concorrência com gorrotinas e canais, por meio de um chat local com múltiplos usuários e um bot que inverte mensagens privadas.

**Origem do enunciado e dos exemplos:** [EP03 no repositório do professor](https://github.com/celsocrivelaro/senac-paradigmas/tree/main/eps/ep03). Os códigos `chat_server.go` e `chat_client.go` foram estudados como referências de comunicação TCP, broadcast e gorrotinas; a implementação abaixo usa um protocolo próprio para cumprir os novos requisitos.

## Arquivos

| Arquivo | Responsabilidade |
| --- | --- |
| `server.go` | Listener TCP, cadastro, roteamento, comandos, anúncios, desconexão e gerenciador concorrente. |
| `client.go` | Entrada de apelido, comandos de terminal e exibição de mensagens recebidas. |
| `bot.go` | Bot independente `Inversor` que responde exclusivamente a mensagens privadas, invertendo runas Unicode. |
| `tests/test_chat.py` | Testes de integração por sockets reais, sem bibliotecas externas. |
| `apresentacao_ep03.pptx` | Slide editável com arquitetura e explicações da implementação. |

**Pré-requisitos:** Go 1.23 ou posterior (utiliza apenas a biblioteca-padrão); terminal no computador local; Python 3.8+ somente para executar os testes opcionais. Não necessita de módulo Go, banco de dados ou serviços externos. A porta `4444` deve estar livre.

## Execução — quatro terminais

Abra os terminais na raiz do projeto:

```bash
# Terminal 1 — servidor
 go run server.go
```

```bash
# Terminal 2 — usuário Zezinho
 go run client.go
# Seu apelido: Zezinho
```

```bash
# Terminal 3 — usuário Hack3r
 go run client.go
# Seu apelido: Hack3r
```

```bash
# Terminal 4 — bot Inversor (não recebe entrada de teclado)
 go run bot.go
```

**Importante:** cada arquivo é um programa autônomo com seu próprio `main`. Execute **`go run server.go`** em vez de `go run .`, pois a pasta contém três funções `main` independentes por exigência do enunciado. Todos usam `localhost:4444` por TCP.

## Comandos do cliente

| Comando digitado | Ação |
| --- | --- |
| `\msg Olá pessoal` | Mensagem pública para todos os **usuários humanos**. |
| `\msg @Zezinho Oi!` | Mensagem privada somente para `@Zezinho`. |
| `\msg @Inversor abcdef` | Mensagem privada ao bot, que responde `fedcba`. |
| `\changenick GatoPreto` | Atualiza o apelido e o endereçamento de mensagens posteriores. |
| `\exit` | Encerra a sessão, liberando a conexão. |

Exemplos das mensagens mostradas no servidor e/ou clientes, de acordo com o destino:

```text
Usuário @Zezinho acabou de entrar
Usuário @Hack3r acabou de entrar
Bot @Inversor acabou de entrar
@Zezinho disse: Olá pessoal
@Zezinho disse em privado: abcdef
Usuário @Hack3r agora é @GatoPreto
Usuário @GatoPreto saiu
```

Para a mensagem privada `abcdef`, o bot imprime:

```text
Mensagem de: @Zezinho
Recebi: abcdef
Resposta: fedcba
```

O usuário remetente recebe: `@Inversor disse em privado: fedcba`.

## Arquitetura e concorrência

```text
Cliente Zezinho ── TCP ──┐
Cliente Hack3r  ── TCP ──┼── Servidor localhost:4444 ── TCP ── Bot Inversor
                         │          │
                         │   canal de eventos
                         │          ▼
                         │   gerenciador (1 gorrotina)
                         │    mapas de participantes
                         │          │
                         └── canais privados de saída (1 por conexão)
                                     ▼
                              escritor TCP por conexão
```

- O processo do servidor aceita conexões em um laço e inicia uma **gorrotina leitora** por conexão.
- A leitora envia eventos para um **canal central**. Uma única **gorrotina gerenciadora** mantém `porApelido` e `ativos`, evitando acessos concorrentes a mapas e canais.
- Cada participante tem um **canal de saída com buffer** e uma **gorrotina escritora** exclusiva; duas operações nunca escrevem simultaneamente no mesmo socket.
- Mensagens públicas e eventos são encaminhados apenas para `USER`. Mensagens privadas são encaminhadas apenas ao destinatário, seja `USER` ou `BOT`.
- Um cliente cuja fila está cheia é desconectado para evitar bloqueio global; escritas TCP têm deadline de cinco segundos. Na desconexão, o gerenciador remove o participante e fecha uma vez seu canal de saída.
- O bot recebe somente quadros `PRIVATE` e responde com `PRIV`; a conversão em `[]rune` evita corromper a codificação UTF-8 de letras acentuadas. A inversão ocorre por runas (não por agrupamentos de grafemas complexos).

## Protocolo TCP textual

Uma mensagem por linha terminada por `\n`, codificada em UTF-8. Campos separados por `TAB` (`\t`). Os comandos públicos `\msg`, `\changenick` e `\exit` são tratados no cliente e convertidos para quadros internos. O servidor usa **a identidade associada à conexão** como remetente; não confia em remetentes informados pelo cliente.

| Origem | Formato de quadro | Significado |
| --- | --- | --- |
| Cliente/bot | `JOIN\tUSER\tZezinho` ou `JOIN\tBOT\tInversor` | Identificação obrigatória como primeira linha. |
| Cliente | `PUB\tOlá pessoal` | Mensagem pública. |
| Cliente/bot | `PRIV\t@Destino\tMensagem` | Mensagem privada. |
| Cliente | `NICK\tGatoPreto` | Troca de apelido. |
| Cliente/bot | `QUIT` | Saída voluntária. |
| Servidor → usuário | `INFO\tTexto formatado` | Entrada, saída, renomeação e mensagens públicas. |
| Servidor → destinatário | `PRIVATE\tRemetente\tMensagem` | Mensagem privada (inclui o remetente para o bot responder). |
| Servidor → usuário | `ERR\tDescrição do erro` | Validação/erro. |

A primeira linha deve ser `JOIN`; cadastro duplicado ou malformado recebe `ERR` e desconexão. Após cadastro, **bots não recebem `INFO` nem `ERR`**, apenas `PRIVATE`. Apelidos devem ter de 1 a 24 caracteres Unicode alfanuméricos ou `_`, sem espaço ou `@`; comparação de unicidade e endereçamento ignora diferenças de maiúsculas/minúsculas. Mensagens vazias e comandos desconhecidos são rejeitados. Scanner limitado a 64 KiB por linha.

## Testes executados

Na validação local em **18/11/2026**, os três arquivos foram compilados individualmente com Go 1.23.2; `go vet` foi executado; houve **16 testes automatizados aprovados**, cobrindo os cenários T01–T16 (T07 e T08 na mesma função) e um teste adicional de comandos inválidos. Os mesmos testes também foram executados com o servidor e bot compilados com `-race`, sem relatórios de corrida nos cenários cobertos.

| Cenário | Evidência resumida | Resultado |
| --- | --- | --- |
| T01/T02/T03 | Entrada de um/dois humanos e identificação de bot | Aprovado |
| T04/T05 | Broadcast aos humanos; bot não recebe `INFO` | Aprovado |
| T06 | Destinatário privado isolado; registro do servidor | Aprovado |
| T07/T08 | Bot recebe, imprime e devolve texto invertido | Aprovado |
| T09/T10 | Mudança de apelido e saída | Aprovado |
| T11/T12 | Queda inesperada e destinatário inexistente | Aprovado |
| T13/T14 | Oito clientes simultâneos (64 mensagens); apelido duplicado | Aprovado |
| T15/T16 | UTF-8 e desconexão durante envios | Aprovado |
| Extra | Comando desconhecido, mensagem vazia, apelido ocupado | Aprovado |

Para reproduzir, **não deixe um servidor já rodando na porta 4444**:

```bash
# Conferências individuais de Go
 gofmt -w server.go client.go bot.go
 go vet server.go
 go vet client.go
 go vet bot.go
 go build -o /tmp/ep03-server server.go
 go build -o /tmp/ep03-client client.go
 go build -o /tmp/ep03-bot bot.go

# Testes TCP reais
 python3 -m unittest discover -s tests -v

# Testes TCP reais sob o detector de corrida
 EP03_RACE=1 python3 -m unittest discover -s tests -v
```

> **Limites da validação:** não existe garantia absoluta de ausência de corridas ou deadlocks em toda execução possível. Os testes foram executados em ambiente Linux com Go 1.23.2; uma demonstração manual nos quatro terminais também é recomendada antes da avaliação presencial.

## Entrega acadêmica e atribuição

- O repositório deve permanecer **público** e o slide `apresentacao_ep03.pptx` deve ser enviado conforme orientação do professor; este README não substitui o slide.
- [Política de uso de ferramentas generativas de IA](https://app.notion.com/p/Pol-tica-de-uso-de-ferramentas-generativas-de-IA-1b53bb4e12a54b4aa06eaa02e62192f4?pvs=21)
- [Política antiplágio](https://app.notion.com/p/Pol-tica-antipl-gio-5187d7b1ab514bfb8424ac0fcfb59dba?pvs=21)

**Pendente de conferência pelo responsável pela entrega:** o conteúdo integral das duas políticas acima não pôde ser consultado durante a implementação. Houve apoio de ferramenta generativa de IA no desenvolvimento; a admissibilidade desse apoio, o dever de declaração e os critérios de autoria precisam ser conferidos nas políticas acadêmicas antes de submeter. Não se declara conformidade com essas políticas sem leitura.
