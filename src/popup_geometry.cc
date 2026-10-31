#include "popup_geometry.h"
namespace tailui {
PopupPlacement placePopup(const QRect &window,const QRect &anchor,const QSize &wanted,int margin,int arrow) {
    PopupPlacement out;
    if(!window.isValid()||!anchor.isValid()||wanted.isEmpty()||margin<0||arrow<0)return out;
    const int width=qMin(wanted.width(),window.width()-2*margin);
    const int top=qMax(window.top()+margin,anchor.bottom()+1);
    const int height=qMin(wanted.height(),window.bottom()-margin-top+1);
    if(width<=2*arrow+5||height<=arrow+4)return out;
    const int left=qBound(window.left()+margin,anchor.center().x()-width+3*margin,window.right()-margin-width+1);
    out.frame=QRect(left,top,width,height);
    out.tipX=qBound(arrow+2,anchor.center().x()-left,width-arrow-3);
    return out;
}
}
