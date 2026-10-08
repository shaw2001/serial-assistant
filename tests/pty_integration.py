"""Exercise the production serialport native binding against two kernel PTYs."""
import json
import os
import select
import subprocess
import tempfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

def main():
    if os.name != 'posix':
        raise SystemExit('PTY integration requires Linux/macOS; Windows USB tests remain manual.')
    m1, s1 = os.openpty()
    m2, s2 = os.openpty()
    p1, p2 = os.ttyname(s1), os.ttyname(s2)
    with tempfile.TemporaryDirectory(prefix='serial-pty-') as logs:
        proc = subprocess.Popen(['node', str(ROOT / 'tests/pty-runner.cjs'), p1, p2, logs], cwd=ROOT, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, bufsize=1)
        def result():
            if not select.select([proc.stdout], [], [], 10)[0]:
                raise AssertionError('Native serial test did not respond within 10 seconds')
            reply = json.loads(proc.stdout.readline())
            assert 'error' not in reply, reply
            return reply
        def command(**args):
            proc.stdin.write(json.dumps(args) + '\n')
            proc.stdin.flush()
            return result()
        def read(fd, length):
            received = bytearray()
            until = time.monotonic() + 5
            while len(received) < length and time.monotonic() < until:
                if select.select([fd], [], [], .1)[0]:
                    received.extend(os.read(fd, length - len(received)))
            assert len(received) == length, (length, len(received))
            return bytes(received)
        try:
            assert result()['ready']
            raw = bytes(range(256))
            command(op='send', id='session-1', config={'content': raw.hex(), 'mode':'hex', 'ending':'none', 'encoding':'utf-8'})
            assert read(m1, 256) == raw
            assert not select.select([m2], [], [], .05)[0], 'TX leaked into the other device'
            os.write(m1, raw)
            phrase = '温度=24.6\r\n'.encode()
            os.write(m2, phrase[:1]);time.sleep(.02);os.write(m2, phrase[1:5]);time.sleep(.02);os.write(m2, phrase[5:])
            time.sleep(.08)
            one = command(op='view', id='session-1')
            two = command(op='view', id='session-2')
            assert bytes.fromhex(one['rxHex']) == raw
            assert bytes.fromhex(two['rxHex']) == phrase
            assert one['state']['tx'] == 256 and two['state']['tx'] == 0
            saved = command(op='export', id='session-1')['records']
            import base64
            assert b''.join(base64.b64decode(r['dataBase64']) for r in saved if r['direction']=='RX') == raw
            command(op='periodic', id='session-1', config={'content':'P','mode':'text','ending':'none','encoding':'utf-8'}, interval=25)
            command(op='periodic', id='session-2', config={'content':'Q','mode':'text','ending':'none','encoding':'utf-8'}, interval=25)
            assert read(m1, 3) == b'PPP' and read(m2, 3) == b'QQQ'
            command(op='stop', id='session-1')
            for _ in range(10):
                command(op='close', id='session-1');command(op='open', id='session-1', path=p1)
            assert command(op='view', id='session-1')['state']['periodic'] is None
            os.close(m1);m1 = None
            time.sleep(.1)
            state = command(op='view', id='session-1')['state']
            assert state['status'] in ('error','disconnected'), state
            assert command(op='view', id='session-2')['state']['status'] == 'connected'
            assert read(m2, 3) == b'QQQ'
            command(op='quit')
            proc.wait(timeout=5)
            assert proc.returncode == 0, proc.stderr.read()
            print('PASS: native serialport + two kernel PTYs; 00..FF bidirectional bytes; fragmented Chinese; isolated periodic TX; lossless disk export; ten reconnects; unplug isolation.')
        finally:
            if proc.poll() is None:
                proc.kill();proc.wait()
            for fd in [m1,s1,m2,s2]:
                if fd is not None:
                    os.close(fd)

if __name__ == '__main__':
    main()
