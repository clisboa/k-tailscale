#include <QApplication>
#include <QImage>
#include <QPainter>
#include <cstdio>
#include <cstdlib>
#include "status_glyph.h"
#include "popup_geometry.h"

#define CHECK(x) do { if (!(x)) { std::fprintf(stderr,"FAIL line %d: %s\n",__LINE__,#x);std::exit(1); } } while(false)
static QImage glyph(bool connected,bool attention){QImage out(84,84,QImage::Format_RGB32);out.fill(Qt::white);QPainter p(&out);p.scale(2,2);tailui::paintStatusGlyph(p,connected,attention);return out;}
int main(int argc,char **argv){
    QApplication app(argc,argv);
    const bool expected[3][3]={{true,true,true},{false,true,false},{false,true,false}};
    const QImage connected=glyph(true,false),disconnected=glyph(false,false),warning=glyph(true,true);
    for(int row=0;row<3;++row)for(int col=0;col<3;++col){
        CHECK((qGray(connected.pixel(14+col*28,14+row*28))<32)==expected[row][col]);
        CHECK(qGray(disconnected.pixel(14+col*28,14+row*28))>230);
        if(expected[row][col])CHECK(qGray(warning.pixel(14+col*28,14+row*28))<32);
    }
    CHECK(qGray(disconnected.pixel(14,7))<128);
    for(int y=0;y<84;++y)CHECK(qGray(connected.pixel(0,y))>230&&qGray(connected.pixel(83,y))>230);
    using tailui::placePopup;
    const auto portrait=placePopup(QRect(0,0,1264,1680),QRect(1080,20,80,90),QSize(760,400),24,12);
    CHECK(portrait.frame.width()==760&&portrait.frame.height()==400&&portrait.frame.top()==110);
    CHECK(QRect(24,24,1216,1632).contains(portrait.frame));
    CHECK(portrait.frame.x()+portrait.tipX==1119);
    const auto landscape=placePopup(QRect(0,0,1680,1264),QRect(1540,20,80,90),QSize(760,2000),24,12);
    CHECK(landscape.frame.height()==1130&&landscape.frame.bottom()==1239);
    CHECK(QRect(24,24,1632,1216).contains(landscape.frame));
    const auto left=placePopup(QRect(0,0,300,500),QRect(0,10,10,50),QSize(760,400),24,12);
    CHECK(left.frame.width()==252&&left.frame.left()==24&&left.tipX>=14);
    CHECK(left.frame.right()<=275&&left.frame.bottom()<=475);
    CHECK(placePopup(QRect(0,0,100,100),QRect(10,90,20,10),QSize(50,200),24,12).frame.isEmpty());
    CHECK(placePopup(QRect(0,0,1264,1680),QRect(),QSize(760,400),24,12).frame.isEmpty());
    CHECK(connected.save("build/connected-T.png"));CHECK(disconnected.save("build/disconnected-dots.png"));
    std::puts("Five-dot T, independent attention, and bounded popup placement tests passed.");
}
