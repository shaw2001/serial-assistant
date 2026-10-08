import os, pty, subprocess, threading, time, select
pairs=[pty.openpty(),pty.openpty()]
env=os.environ.copy();env['SERIAL_TEST_PTY_A']=os.ttyname(pairs[0][1]);env['SERIAL_TEST_PTY_B']=os.ttyname(pairs[1][1])
process=subprocess.Popen(['go','test','-v','./internal/core','-run','^TestNativePTY$','-count=1'],cwd='native',env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True)
ready=threading.Event();sent=threading.Event();reopened=threading.Event()
def reader():
    for line in process.stdout:
        print(line,end='',flush=True)
        if 'PTY READY' in line: ready.set()
        if 'PTY TX COMPLETE' in line: sent.set()
        if 'PTY RECONNECT COMPLETE' in line: reopened.set()
threading.Thread(target=reader,daemon=True).start()
assert ready.wait(30),'backend did not open PTYs'
raw=b'\x00\xff'+ '温度\r\n'.encode()
for value in raw: os.write(pairs[0][0],bytes([value]));time.sleep(.002)
os.write(pairs[1][0],b'port2')
assert sent.wait(10),'TX not complete'
def drain(fd,expected):
    data=b'';deadline=time.time()+3
    while len(data)<len(expected) and time.time()<deadline:
        if select.select([fd],[],[],.1)[0]:data+=os.read(fd,65536)
    assert data==expected,(data,expected)
drain(pairs[0][0],raw);drain(pairs[1][0],b'port2')
assert reopened.wait(15),'reconnect did not finish'
os.close(pairs[0][0]);pairs[0]=(None,pairs[0][1])
code=process.wait(timeout=10)
for master,slave in pairs:
    if master is not None:os.close(master)
    os.close(slave)
assert code==0,code
print('PASS: Go native serial, two kernel PTYs, binary + fragmented Chinese, separate TX, disk export, reconnect and unplug')
