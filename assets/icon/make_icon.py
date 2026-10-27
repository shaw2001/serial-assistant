import numpy as np
from PIL import Image, ImageFilter
from scipy import ndimage
# Usage: python make_icon.py <source.png>  (needs numpy, scipy, pillow)
import sys
src=np.asarray(Image.open(sys.argv[1] if len(sys.argv)>1 else 'source.png').convert('RGB')).astype(np.float32)
d=(255-src).max(axis=2)                      # distance from white
near=d<10
lab,_=ndimage.label(near)
border=set(np.unique(np.concatenate([lab[0],lab[-1],lab[:,0],lab[:,-1]])))-{0}
bg=np.isin(lab,list(border))
dist=ndimage.distance_transform_edt(~bg)     # px from background
band=(dist>0)&(dist<=5)
a=np.ones(d.shape,np.float32); a[bg]=0
a[band]=np.clip(d[band]/150.0,0,1)
rgb=src.copy()
m=band&(a>0)
for c in range(3):
    ch=rgb[...,c]; ch[m]=np.clip((ch[m]-(1-a[m])*255)/a[m],0,255)
out=np.dstack([rgb,a*255]).astype(np.uint8)
ys,xs=np.where(a>0.02); y0,y1,x0,x1=ys.min(),ys.max()+1,xs.min(),xs.max()+1
print('bbox',x0,y0,x1,y1)
im=Image.fromarray(out,'RGBA').crop((x0,y0,x1,y1))
s=max(im.size); pad=int(s*0.02); S=s+2*pad
c=Image.new('RGBA',(S,S),(0,0,0,0)); c.paste(im,((S-im.size[0])//2,(S-im.size[1])//2)); im=c
def rs(n):
    # premultiplied Lanczos to avoid dark/white fringes
    arr=np.asarray(im).astype(np.float32)/255; pm=arr.copy(); pm[...,:3]*=pm[...,3:]
    r=np.dstack([np.asarray(Image.fromarray((pm[...,i]*65535).astype(np.uint16).astype(np.int32),'I').convert('F').resize((n,n),Image.LANCZOS)) for i in range(4)])/65535
    r=np.clip(r,0,1); al=r[...,3:]; rgb=np.where(al>1e-4,r[...,:3]/np.maximum(al,1e-4),0)
    o=Image.fromarray((np.dstack([np.clip(rgb,0,1),al])*255+.5).astype(np.uint8),'RGBA')
    if n<=48:
        amt={16:120,24:90,32:70,48:50}[n]
        sh=o.filter(ImageFilter.UnsharpMask(radius=0.6,percent=amt,threshold=1))
        o=Image.merge('RGBA',(*sh.split()[:3],o.split()[3]))
    return o
import os
dst='/workspace/serial-assistant/assets/icon'
for f in os.listdir(dst): os.remove(os.path.join(dst,f))
rs(1024).save(f'{dst}/icon_1024.png')
sizes=[16,24,32,48,64,128,256]
imgs={n:rs(n) for n in sizes}
for n,i in imgs.items(): i.save(f'{dst}/icon_{n}.png')
rs(60).save(f'{dst}/logo_60.png',optimize=True)
imgs[256].save(f'{dst}/icon.ico',format='ICO',sizes=[(n,n) for n in sizes],append_images=[imgs[n] for n in sizes[:-1]])
# preview sheet
show=[16,24,32,48,256]; W=sum(show)+40*(len(show)+1); H=2*(256+60)
sheet=Image.new('RGBA',(W,H))
for row,bgc in enumerate([(255,255,255,255),(32,33,36,255)]):
    sheet.paste(Image.new('RGBA',(W,H//2),bgc),(0,row*H//2)); x=40
    for n in show:
        sheet.alpha_composite(imgs[n],(x,row*H//2+30+(256-n)//2)); x+=n+40
sheet.save('sizes_preview.png')
