"""Testes de aceitação TCP, sem bibliotecas de terceiros.

Execute no diretório raiz: python3 -m unittest discover -s tests -v
O teste inicia um servidor próprio em localhost:4444; não deixe outro ativo.
"""
import os
import queue
import socket
import subprocess
import tempfile
import threading
import time
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PORT = ("localhost", 4444)


class Peer:
    def __init__(self, name, kind="USER"):
        self.socket = socket.create_connection(PORT, timeout=3)
        self.socket.settimeout(0.2)
        self.received = queue.Queue()
        self.stop = threading.Event()
        self.thread = threading.Thread(target=self._read, daemon=True)
        self.thread.start()
        self.send(f"JOIN\t{kind}\t{name}")

    def _read(self):
        buffer = b""
        while not self.stop.is_set():
            try:
                fragment = self.socket.recv(8192)
                if not fragment:
                    break
                buffer += fragment
                while b"\n" in buffer:
                    part, buffer = buffer.split(b"\n", 1)
                    self.received.put(part.decode("utf-8").rstrip("\r"))
            except socket.timeout:
                continue
            except OSError:
                break

    def send(self, frame):
        self.socket.sendall((frame + "\n").encode("utf-8"))

    def expect(self, frame, timeout=3):
        deadline = time.monotonic() + timeout
        saw = []
        while time.monotonic() < deadline:
            try:
                item = self.received.get(timeout=min(0.1, deadline - time.monotonic()))
            except queue.Empty:
                continue
            saw.append(item)
            if item == frame:
                return
        raise AssertionError(f"Não recebeu {frame!r}; recebeu {saw!r}")

    def no_frame(self, frame, duration=0.4):
        deadline = time.monotonic() + duration
        unexpected = []
        while time.monotonic() < deadline:
            try:
                item = self.received.get(timeout=min(0.05, deadline - time.monotonic()))
                unexpected.append(item)
            except queue.Empty:
                pass
        if frame in unexpected:
            raise AssertionError(f"Quadro indevido recebido: {frame!r}")

    def close(self, voluntary=True):
        if voluntary:
            try:
                self.send("QUIT")
            except OSError:
                pass
        self.stop.set()
        try:
            self.socket.shutdown(socket.SHUT_RDWR)
        except OSError:
            pass
        self.socket.close()
        self.thread.join(timeout=1)


class ChatAcceptance(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        subprocess.run(["go", "build", *(["-race"] if os.getenv("EP03_RACE") else []), "-o", "/tmp/ep03-test-server", "server.go"], cwd=ROOT, check=True)
        subprocess.run(["go", "build", *(["-race"] if os.getenv("EP03_RACE") else []), "-o", "/tmp/ep03-test-bot", "bot.go"], cwd=ROOT, check=True)
        cls.server_log = tempfile.TemporaryFile(mode="w+t", encoding="utf-8")
        cls.server = subprocess.Popen(["/tmp/ep03-test-server"], cwd=ROOT, stdout=cls.server_log, stderr=subprocess.STDOUT)
        for _ in range(100):
            if cls.server.poll() is not None:
                raise RuntimeError("Servidor terminou antes de escutar porta 4444")
            try:
                with socket.create_connection(PORT, timeout=0.05):
                    break
            except OSError:
                time.sleep(0.05)
        else:
            raise RuntimeError("Servidor não abriu localhost:4444")

    @classmethod
    def tearDownClass(cls):
        cls.server.terminate()
        try:
            cls.server.wait(timeout=5)
        except subprocess.TimeoutExpired:
            cls.server.kill()
        cls.server_log.close()

    def setUp(self):
        self.peers = []

    def tearDown(self):
        for peer in reversed(self.peers):
            peer.close()
        time.sleep(0.03)

    def join(self, name, kind="USER"):
        p = Peer(name, kind)
        self.peers.append(p)
        if kind == "USER":
            p.expect(f"INFO\tUsuário @{name} acabou de entrar")
        return p

    def test_01_entrada_usuario(self):
        self.join("Zezinho")

    def test_02_dois_usuarios(self):
        a = self.join("Zezinho")
        self.join("Hack3r")
        a.expect("INFO\tUsuário @Hack3r acabou de entrar")

    def test_03_bot_identificado(self):
        a = self.join("Zezinho")
        bot = self.join("TesteBot", "BOT")
        a.expect("INFO\tBot @TesteBot acabou de entrar")
        bot.no_frame("INFO\tBot @TesteBot acabou de entrar")

    def test_04_publica(self):
        a = self.join("Zezinho")
        b = self.join("Hack3r")
        a.send("PUB\tOlá pessoal")
        expected = "INFO\t@Zezinho disse: Olá pessoal"
        a.expect(expected)
        b.expect(expected)

    def test_05_bot_nao_recebe_publica(self):
        a = self.join("Zezinho")
        bot = self.join("Inversor", "BOT")
        a.send("PUB\tNão entregue ao bot")
        a.expect("INFO\t@Zezinho disse: Não entregue ao bot")
        bot.no_frame("INFO\t@Zezinho disse: Não entregue ao bot")
        self.assertTrue(bot.received.empty(), "Bot recebeu quadro fora de PRIVATE")

    def test_06_privada_isolada(self):
        a = self.join("Zezinho")
        b = self.join("Hack3r")
        c = self.join("Terceiro")
        a.send("PRIV\t@Hack3r\tSegredo")
        b.expect("PRIVATE\tZezinho\tSegredo")
        a.no_frame("PRIVATE\tZezinho\tSegredo")
        c.no_frame("PRIVATE\tZezinho\tSegredo")
        self.server_log.flush()
        self.server_log.seek(0)
        self.assertIn("@Zezinho disse em privado: Segredo", self.server_log.read())
        self.server_log.seek(0, os.SEEK_END)

    def test_07_08_bot_reage_inverte_e_responde(self):
        a = self.join("Zezinho")
        bot = subprocess.Popen(["/tmp/ep03-test-bot"], cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        try:
            a.expect("INFO\tBot @Inversor acabou de entrar")
            a.send("PRIV\t@Inversor\tabcdef")
            a.expect("PRIVATE\tInversor\tfedcba")
            a.send("PRIV\t@Inversor\táéç")
            a.expect("PRIVATE\tInversor\tçéá")
        finally:
            bot.terminate()
            out, _ = bot.communicate(timeout=5)
            self.assertIn("Mensagem de: @Zezinho", out)
            self.assertIn("Recebi: abcdef", out)
            self.assertIn("Resposta: fedcba", out)

    def test_09_alterar_apelido_e_enderecar(self):
        a = self.join("Zezinho")
        b = self.join("Hack3r")
        b.send("NICK\tGatoPreto")
        a.expect("INFO\tUsuário @Hack3r agora é @GatoPreto")
        a.send("PRIV\t@GatoPreto\tNovo nome")
        b.expect("PRIVATE\tZezinho\tNovo nome")
        b.send("PUB\tMudei")
        a.expect("INFO\t@GatoPreto disse: Mudei")

    def test_10_saida_voluntaria(self):
        a = self.join("Zezinho")
        b = self.join("GatoPreto")
        b.send("QUIT")
        a.expect("INFO\tUsuário @GatoPreto saiu")

    def test_11_desconexao_inesperada(self):
        a = self.join("Zezinho")
        b = self.join("Hack3r")
        b.close(voluntary=False)
        a.expect("INFO\tUsuário @Hack3r saiu")
        c = self.join("Outro")
        a.send("PUB\tServidor ativo")
        c.expect("INFO\t@Zezinho disse: Servidor ativo")

    def test_12_destinatario_inexistente(self):
        a = self.join("Zezinho")
        a.send("PRIV\t@Fantasma\tOi")
        a.expect("ERR\tdestinatário não encontrado")
        a.send("PUB\tContinuo aqui")
        a.expect("INFO\t@Zezinho disse: Continuo aqui")

    def test_13_concorrencia_multiplos_clientes(self):
        users = [self.join(f"Cliente{i}") for i in range(8)]
        def enviar(i):
            for j in range(8):
                users[i].send(f"PUB\t{i}-{j}")
        threads = [threading.Thread(target=enviar, args=(i,)) for i in range(8)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()
        expected = {f"INFO\t@Cliente{i} disse: {i}-{j}" for i in range(8) for j in range(8)}
        for p in users:
            seen = set()
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline and len(seen) < len(expected):
                try:
                    frame = p.received.get(timeout=0.1)
                except queue.Empty:
                    continue
                if frame in expected:
                    seen.add(frame)
            self.assertEqual(seen, expected)

    def test_14_apelido_duplicado(self):
        a = self.join("Zezinho")
        b = Peer("ZEZINHO")
        self.peers.append(b)
        b.expect("ERR\tapelido já está em uso")
        a.send("PUB\tSem conflito")
        a.expect("INFO\t@Zezinho disse: Sem conflito")

    def test_15_unicode(self):
        a = self.join("Zezinho")
        b = self.join("Hack3r")
        a.send("PUB\tOlá, você está aí? 🐱")
        b.expect("INFO\t@Zezinho disse: Olá, você está aí? 🐱")
        a.send("PRIV\t@Hack3r\tCoração ❤️")
        b.expect("PRIVATE\tZezinho\tCoração ❤️")

    def test_16_desconexao_durante_envios(self):
        a = self.join("Zezinho")
        b = self.join("Hack3r")
        def flood():
            for j in range(50):
                try:
                    a.send(f"PUB\tMensagem {j}")
                except OSError:
                    break
        t = threading.Thread(target=flood)
        t.start()
        b.close(voluntary=False)
        t.join()
        a.expect("INFO\tUsuário @Hack3r saiu")
        self.assertIsNone(self.server.poll())

    def test_17_comandos_invalidos_e_nick_duplicado(self):
        a = self.join("Zezinho")
        self.join("Hack3r")
        a.send("NICK\tHack3r")
        a.expect("ERR\tapelido já está em uso")
        a.send("PUB\t")
        a.expect("ERR\tmensagem vazia")
        a.send("OQUE")
        a.expect("ERR\tcomando desconhecido")


if __name__ == "__main__":
    unittest.main()
