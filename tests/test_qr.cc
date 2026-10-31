#include "qr_code.h"
#include "qr_widget.h"
#include <QEvent>
#include <QApplication>
#include <QFile>
#include <cstdio>
#include <cstdlib>
#define CHECK(x) do { if(!(x)){std::fprintf(stderr,"FAIL line %d: %s\n",__LINE__,#x);std::exit(1);} } while(false)
static void imageChecks(const QImage &image,int limit){
    CHECK(!image.isNull());CHECK(image.width()==image.height()&&image.width()<=limit);
    int left=image.width(),top=image.height();
    for(int y=0;y<image.height();++y)for(int x=0;x<image.width();++x){
        const QRgb color=image.pixel(x,y);CHECK(color==qRgb(0,0,0)||color==qRgb(255,255,255));
        if(color==qRgb(0,0,0)){left=qMin(left,x);top=qMin(top,y);}
    }
    CHECK(left==top&&left<image.width()/2);
    int run=0;while(left+run<image.width()&&image.pixel(left+run,top)==qRgb(0,0,0))++run;
    CHECK(run%7==0);const int scale=run/7;CHECK(scale>=4&&left>=scale*4);
    CHECK(image.width()%scale==0);
    for(int y=0;y<image.height();++y)for(int x=0;x<image.width();++x)
        if(x<scale*4||y<scale*4||x>=image.width()-scale*4||y>=image.height()-scale*4)CHECK(image.pixel(x,y)==qRgb(255,255,255));
}
static void save(const char *name,const QString &url,int pixels){
    const QImage image=tailui::loginQr(url,pixels);imageChecks(image,pixels);
    CHECK(image.save(QString("build/qr-")+name+".png"));
    QFile payload(QString("build/qr-")+name+".txt");CHECK(payload.open(QIODevice::WriteOnly));CHECK(payload.write(url.toUtf8())==url.toUtf8().size());
}
class PaintProbe : public QObject {
public:
    explicit PaintProbe(QObject *target):target_(target){}
    int paints=0;
    bool eventFilter(QObject *object,QEvent *event) override {
        if(object==target_&&event->type()==QEvent::Paint)++paints;
        return false;
    }
private: QObject *target_;
};
int main(int argc,char **argv){
    QApplication app(argc,argv);
    const QString url="https://login.tailscale.com/a/example0123456789";
    CHECK(!tailui::loginQr(url,420).isNull());
    save("plain",url,420);
    save("query","https://login.tailscale.com/a/example?next=%2Fpath%3Fa%3D1&token=A%2Bb%26C",420);
    save("small",url,220);
    save("changed","https://login.tailscale.com/a/replacement9876543210",420);
    CHECK(tailui::loginQr("",420).isNull());
    CHECK(tailui::loginQr("javascript:alert(1)",420).isNull());
    CHECK(tailui::loginQr("https://",420).isNull());
    CHECK(tailui::loginQr(url+QChar(0)+"suffix",420).isNull());
    CHECK(tailui::loginQr(url,115).isNull());
    CHECK(tailui::loginQr(url,-1).isNull());
    CHECK(tailui::loginQr(url+QString(1500,'a'),420).isNull());
    CHECK(tailui::loginQr(url+QString(2500,'a'),420).isNull());
    QWidget window;window.resize(600,600);tailui::QrWidget widget(&window);
    CHECK(widget.setUrl(url,420));widget.show();window.show();QApplication::processEvents();
    imageChecks(widget.grab().toImage(),420);
    const qint64 key=widget.image().cacheKey();const QSize size=widget.size();
    PaintProbe probe(&widget);app.installEventFilter(&probe);
    CHECK(widget.setUrl(url,420));QApplication::processEvents();
    CHECK(probe.paints==0&&widget.size()==size&&widget.image().cacheKey()==key);
    const QString changed="https://login.tailscale.com/a/widget-replacement0123";
    CHECK(widget.setUrl(changed,420));QApplication::processEvents();CHECK(widget.image().cacheKey()!=key);
    CHECK(widget.grab().save("build/qr-widget.png"));QFile payload("build/qr-widget.txt");CHECK(payload.open(QIODevice::WriteOnly));CHECK(payload.write(changed.toUtf8())==changed.toUtf8().size());payload.close();
    CHECK(!widget.setUrl("",420));CHECK(widget.image().isNull()&&widget.size().isEmpty());
    CHECK(!widget.setUrl(url+QString(2500,'a'),420));CHECK(widget.image().isNull());
    app.removeEventFilter(&probe);
    std::puts("QR payload images, readable bounds, quiet zone and fallback tests passed.");
}
