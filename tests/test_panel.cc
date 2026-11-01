#include <QApplication>
#include <QElapsedTimer>
#include <QEvent>
#include <QFile>
#include <QLabel>
#include <QWindow>
#include <QJsonDocument>
#include <QJsonArray>
#include <QJsonObject>
#include <QLocalServer>
#include <QLocalSocket>
#include <QPushButton>
#include <QTemporaryDir>
#include <QThread>
#include <QTouchDevice>
#include <QTouchEvent>
#include <QVariant>
#include <QMouseEvent>
#include <QScrollArea>
#include <QScrollBar>
#include <cstdio>
#include <cstdlib>
#include <functional>
#include "controller.h"
#include "panel.h"
#include "touch_button.h"
#include "qr_widget.h"

#define CHECK(x) do { if (!(x)) { std::fprintf(stderr,"FAIL line %d: %s\n",__LINE__,#x); std::exit(1); } } while(false)
static bool waitFor(const std::function<bool()> &condition,int ms=4500) {
    QElapsedTimer timer;timer.start();
    while (!condition() && timer.elapsed()<ms) { QApplication::processEvents();QThread::msleep(3); }
    return condition();
}
static void spin(int ms){QElapsedTimer t;t.start();while(t.elapsed()<ms){QApplication::processEvents();QThread::msleep(3);}}
static QByteArray read(const QString &path){QFile f(path);if(!f.open(QIODevice::ReadOnly))return {};return f.readAll();}
static int centerPixels(const QImage &image,int edge,int low,int high) {
    int count=0;
    for(int y=(image.height()-edge)/2;y<(image.height()+edge)/2;++y)
        for(int x=(image.width()-edge)/2;x<(image.width()+edge)/2;++x) {
            const int gray=qGray(image.pixel(x,y));if(gray>=low&&gray<=high)++count;
        }
    return count;
}
class PaintProbe : public QObject {
public:
    explicit PaintProbe(QWidget *root):root_(root){}
    int paints=0,layouts=0,resizes=0,panelPaints=0,updateRequests=0,paintFrames=0;bool painted=false;
    bool eventFilter(QObject *object,QEvent *event) override {
        if(object==root_&&event->type()==QEvent::Paint)++panelPaints;
        if(object==root_->window()&&event->type()==QEvent::UpdateRequest){++updateRequests;painted=false;}
        QWidget *w=qobject_cast<QWidget*>(object);
        if(w&&(w==root_||root_->isAncestorOf(w))){
            if(event->type()==QEvent::Paint){++paints;if(!painted){painted=true;++paintFrames;}}
            if(event->type()==QEvent::LayoutRequest)++layouts;
            if(event->type()==QEvent::Resize)++resizes;
        }
        return false;
    }
private: QWidget *root_;
};
static void mouseTap(QWidget *widget,Qt::MouseEventSource source=Qt::MouseEventNotSynthesized){
    QPointF pos=widget->rect().center(),global=widget->mapToGlobal(pos.toPoint()),window=widget->mapTo(widget->window(),pos.toPoint());
    QMouseEvent press(QEvent::MouseButtonPress,pos,window,global,Qt::LeftButton,Qt::LeftButton,Qt::NoModifier,source);QApplication::sendEvent(widget,&press);
    QMouseEvent release(QEvent::MouseButtonRelease,pos,window,global,Qt::LeftButton,Qt::NoButton,Qt::NoModifier,source);QApplication::sendEvent(widget,&release);
}
static void touchTap(QWidget *widget){
    QTouchDevice device;device.setType(QTouchDevice::TouchScreen);QTouchEvent::TouchPoint point(0);point.setPos(widget->rect().center());point.setScreenPos(widget->mapToGlobal(widget->rect().center()));point.setState(Qt::TouchPointPressed);
    QTouchEvent begin(QEvent::TouchBegin,&device,Qt::NoModifier,Qt::TouchPointPressed,{point});QApplication::sendEvent(widget,&begin);point.setState(Qt::TouchPointReleased);
    QTouchEvent end(QEvent::TouchEnd,&device,Qt::NoModifier,Qt::TouchPointReleased,{point});QApplication::sendEvent(widget,&end);
}
static bool nativeTouchTap(QWidget *widget){
    QTouchDevice device;device.setType(QTouchDevice::TouchScreen);
    QTouchEvent::TouchPoint point(0);point.setPos(widget->mapTo(widget->window(),widget->rect().center()));point.setScreenPos(widget->mapToGlobal(widget->rect().center()));point.setState(Qt::TouchPointPressed);
    QTouchEvent begin(QEvent::TouchBegin,&device,Qt::NoModifier,Qt::TouchPointPressed,{point});QApplication::sendEvent(widget->window()->windowHandle(),&begin);
    point.setState(Qt::TouchPointReleased);QTouchEvent end(QEvent::TouchEnd,&device,Qt::NoModifier,Qt::TouchPointReleased,{point});QApplication::sendEvent(widget->window()->windowHandle(),&end);
    return begin.isAccepted()&&end.isAccepted();
}
static void touchSwipe(QWidget *widget,int dy){
    QTouchDevice device;device.setType(QTouchDevice::TouchScreen);
    QTouchEvent::TouchPoint point(0);point.setPos(widget->rect().center());point.setScreenPos(widget->mapToGlobal(point.pos().toPoint()));point.setState(Qt::TouchPointPressed);
    QTouchEvent begin(QEvent::TouchBegin,&device,Qt::NoModifier,Qt::TouchPointPressed,{point});QApplication::sendEvent(widget,&begin);
    point.setPos(point.pos()+QPointF(0,dy));point.setScreenPos(point.screenPos()+QPointF(0,dy));point.setState(Qt::TouchPointMoved);
    QTouchEvent update(QEvent::TouchUpdate,&device,Qt::NoModifier,Qt::TouchPointMoved,{point});QApplication::sendEvent(widget,&update);
    point.setState(Qt::TouchPointReleased);QTouchEvent end(QEvent::TouchEnd,&device,Qt::NoModifier,Qt::TouchPointReleased,{point});QApplication::sendEvent(widget,&end);
}
int main(int argc,char **argv){
    QApplication app(argc,argv);QTemporaryDir dir;CHECK(dir.isValid());
    const QString signedOutFile=dir.path()+"/signed-out",failConnect=dir.path()+"/fail-connect",failLogout=dir.path()+"/fail-logout",calls=dir.path()+"/calls";
    const QString route=dir.path()+"/route",helper=dir.path()+"/control.sh",last=dir.path()+"/action",allowStop=dir.path()+"/allow-stop",failPrepare=dir.path()+"/fail-prepare";
    QFile f(route);CHECK(f.open(QIODevice::WriteOnly));f.write("Iface Destination Gateway Flags\nwlan0 00000000 0100000A 0003\n");f.close();
    f.setFileName(helper);CHECK(f.open(QIODevice::WriteOnly));f.write(("#!/bin/sh\necho \"$1\" > '"+last+"'\necho \"$1\" >> '"+calls+"'\nif [ \"$1\" = logout ]; then sleep 0.08; if [ -f '"+failLogout+"' ]; then echo 'logout denied'; exit 7; fi; touch '"+signedOutFile+"'; exit 0; fi\nif [ \"$1\" = prepare ]; then sleep 0.05; if [ -f '"+failPrepare+"' ]; then echo 'not enough system space'; exit 4; fi; echo 'system storage ready'; exit 0; fi\nif [ \"$1\" = stop ]; then if [ -f '"+allowStop+"' ]; then echo 'Ready for USB'; exit 0; fi; echo 'daemon still running'; exit 4; fi\nif [ \"$1\" = connect ] || [ \"$1\" = signin ]; then if [ -f '"+failConnect+"' ]; then sleep 0.15; echo 'Sign-in request timed out before a login link was returned.'; exit 4; fi; sleep 0.15; echo 'waiting for approval'; exit 1; fi\n").toUtf8());f.close();
    QLocalServer server;CHECK(server.listen(dir.path()+"/socket"));int requests=0;bool sawPeers=false,malformed=false,approval=false,hold=false,login=false,loginKey=false,loggedOutHealth=false,staleAuth=false;
    QString profile="reader-one";
    QString authUrl="https://login.tailscale.com/a/example0123456789";
    QPointer<QLocalSocket> held;
    QObject::connect(&server,&QLocalServer::newConnection,&app,[&]{
        QLocalSocket *peer=server.nextPendingConnection();
        QObject::connect(peer,&QLocalSocket::readyRead,peer,[&,peer]{
            if(peer->property("handled").toBool()){peer->readAll();return;}
            QByteArray req=peer->property("request").toByteArray()+peer->readAll();peer->setProperty("request",req);
            if(!req.contains("\r\n\r\n"))return;
            peer->setProperty("handled",true);++requests;
            if(hold){held=peer;return;}
            const bool devices=!req.contains("?peers=false");sawPeers=sawPeers||devices;
            const bool stopped=read(last).trimmed()=="disconnect";
            QByteArray json=QByteArray("{\"BackendState\":\"")+(stopped?"Stopped":"Running")+"\",\"Self\":{\"HostName\":\"<b>reader</b>\",\"TailscaleIPs\":[\"100.64.0.8\"]}}";
            // LocalAPI can retain its peer map after tailscale down.
            if(devices)json=QByteArray("{\"BackendState\":\"")+(stopped?"Stopped":"Running")+"\",\"Self\":{\"HostName\":\"<b>reader</b>\",\"TailscaleIPs\":[\"100.64.0.8\"]},\"Peer\":{\"one\":{\"HostName\":\"<b>nas</b>\",\"TailscaleIPs\":[\"100.64.0.2\"],\"Online\":true}}}";
            if(staleAuth){QJsonObject obj=QJsonDocument::fromJson(json).object();obj.insert("AuthURL",authUrl);json=QJsonDocument(obj).toJson();}
            if(login)json=QByteArray("{\"BackendState\":\"NeedsLogin\",\"AuthURL\":\"")+authUrl.toUtf8()+"\"}";
            if(login&&loginKey){QJsonObject obj=QJsonDocument::fromJson(json).object();obj.insert("HaveNodeKey",true);json=QJsonDocument(obj).toJson();}
            if(malformed)json="bad status";
            if(approval)json="{\"BackendState\":\"NeedsMachineAuth\"}";
            if(!malformed&&!approval&&!login){
                QJsonObject obj=QJsonDocument::fromJson(json).object();
                const bool loggedOut=QFile::exists(signedOutFile);obj.insert("HaveNodeKey",!loggedOut);
                QJsonObject self=obj.value("Self").toObject();self.insert("ID",profile);obj.insert("Self",self);
                if(loggedOut){obj.insert("BackendState","NeedsLogin");obj.insert("AuthURL",authUrl);if(loggedOutHealth)obj.insert("Health",QJsonArray{QString("You are logged out.")});}
                json=QJsonDocument(obj).toJson();
            }
            peer->write("HTTP/1.0 200 OK\r\nContent-Length: "+QByteArray::number(json.size())+"\r\n\r\n"+json);peer->disconnectFromServer();
        });
    });
    const QString updater=dir.path()+"/updater", updateCalls=dir.path()+"/update-calls", updateStatus=dir.path()+"/update-status";
    f.setFileName(updateStatus);CHECK(f.open(QIODevice::WriteOnly));f.write("{\"automatic\":true,\"current\":\"1.104.4\",\"latest\":\"1.104.6\",\"available\":true,\"addon_current\":\"0.1.0\",\"addon_latest\":\"0.2.0\",\"addon_available\":true,\"can_install\":true}");f.close();
    f.setFileName(updater);CHECK(f.open(QIODevice::WriteOnly));f.write(("#!/bin/sh\necho \"$1\" >> '"+updateCalls+"'\ncase \"$1\" in checks-off) sed -i 's/\"automatic\":true/\"automatic\":false/' '"+updateStatus+"';; checks-on) sed -i 's/\"automatic\":false/\"automatic\":true/' '"+updateStatus+"';; status) cat '"+updateStatus+"';; check*) sleep 0.03; echo '{}';; esac\n").toUtf8());f.close();CHECK(f.setPermissions(QFile::ReadOwner|QFile::WriteOwner|QFile::ExeOwner));
    const QString wireless=dir.path()+"/wireless";
    f.setFileName(wireless);CHECK(f.open(QIODevice::WriteOnly));f.write("eth0: 0000 70. -40. -256 0 0 0 0 0 0\n");f.close();
    tailui::Controller controller(dir.path()+"/socket",helper,route,nullptr,updater,wireless);
    QWidget window;window.resize(1264,1680);window.setStyleSheet("background:white;");
    QPushButton book("Book underneath",&window);book.setAttribute(Qt::WA_AcceptTouchEvents);book.setGeometry(30,1000,500,150);int bookClicks=0;
    QObject::connect(&book,&QPushButton::clicked,[&]{++bookClicks;});
    QWidget anchor(&window);anchor.setGeometry(1080,20,80,90);window.show();
    tailui::Panel panel(&window,&controller,&anchor);panel.open();
    auto state=panel.findChild<QLabel*>("tailuiState");auto identity=panel.findChild<QLabel*>("tailuiIdentity");
    auto primary=panel.findChild<QPushButton*>("tailuiPrimary");auto updateButton=panel.findChild<QPushButton*>("tailuiUpdates");
    CHECK(state&&identity&&primary&&updateButton);
    CHECK(waitFor([&]{return state->text()=="Connected";}));
    CHECK(panel.findChild<QLabel*>("tailuiTitle")->text()=="K-Tailscale");
    CHECK(panel.width()<window.width()&&panel.height()<window.height()/2);
    CHECK(panel.geometry().top()>anchor.geometry().bottom());
    CHECK(window.rect().adjusted(10,10,-10,-10).contains(panel.geometry()));
    CHECK(!panel.findChild<QPushButton*>("tailuiDevices")&&!panel.findChild<QPushButton*>("tailuiRefresh")&&!panel.findChild<QPushButton*>("tailuiClose"));
    CHECK(identity->textFormat()==Qt::PlainText&&identity->text().contains("<b>reader</b>"));
    CHECK(identity->text().contains("100.64.0.8"));
    CHECK(waitFor([&]{return identity->height()>=identity->fontMetrics().height()+identity->fontMetrics().lineSpacing();}));
    CHECK(!panel.findChild<QWidget*>("tailuiUpdateSection")->isVisible());
    // Confirmation never logs out until the second, explicit tap.
    auto signout=panel.findChild<QPushButton*>("tailuiSignOut");
    auto confirm=panel.findChild<QPushButton*>("tailuiSignOutConfirm");
    auto cancel=panel.findChild<QPushButton*>("tailuiSignOutCancel");
    auto confirmation=panel.findChild<QWidget*>("tailuiSignOutSection");
    auto signoutQr=panel.findChild<tailui::QrWidget*>("tailuiLoginQr");
    auto signoutLink=panel.findChild<QLabel*>("tailuiLogin");
    CHECK(signout&&confirm&&cancel&&confirmation&&signoutQr&&signoutLink);
    CHECK(waitFor([&]{return signout->isVisible()&&signout->isEnabled();}));
    QByteArray beforeSignout=read(calls);signout->click();CHECK(confirmation->isVisible());
    CHECK(read(calls)==beforeSignout);cancel->click();CHECK(!confirmation->isVisible()&&read(calls)==beforeSignout);
    signout->click();CHECK(window.grab().save("build/widget-signout-confirm.png"));
    mouseTap(&book);CHECK(!panel.isVisible());panel.open();CHECK(!confirmation->isVisible()&&read(calls)==beforeSignout);spin(700);
    signout->click();updateButton->click();CHECK(!confirmation->isVisible());updateButton->click();
    signout->click();profile="reader-two";controller.refresh();CHECK(waitFor([&]{return !confirmation->isVisible();}));
    signout->click();CHECK(confirmation->isVisible());controller.updates().check();CHECK(!confirmation->isVisible()&&!signout->isEnabled());
    CHECK(waitFor([&]{return !controller.updates().busy()&&signout->isEnabled();}));
    primary->click();CHECK(waitFor([&]{return !controller.busy()&&!controller.updates().busy()&&state->text()=="Disconnected";}));CHECK(signout->isVisible()&&signout->isEnabled());
    f.setFileName(route);CHECK(f.open(QIODevice::WriteOnly|QIODevice::Truncate));f.write("eth0 00000000 0100000A 0003\n");f.close();controller.refresh();
    CHECK(waitFor([&]{return controller.hasWifi()&&primary->isEnabled();}));
    f.setFileName(route);CHECK(f.open(QIODevice::WriteOnly|QIODevice::Truncate));f.close();controller.refresh();
    CHECK(waitFor([&]{return state->text()=="Wi-Fi unavailable";}));CHECK(signout->isVisible()&&signout->isEnabled());
    CHECK(f.open(QIODevice::WriteOnly|QIODevice::Truncate));f.write("Iface Destination Gateway Flags\nwlan0 00000000 0100000A 0003\n");f.close();controller.refresh();
    CHECK(waitFor([&]{return state->text()=="Disconnected"&&!controller.updates().busy();}));
    // Errors preserve the profile and remain actionable.
    f.setFileName(failLogout);CHECK(f.open(QIODevice::WriteOnly));f.close();
    signout->click();confirm->click();CHECK(controller.busy());
    CHECK(waitFor([&]{return !controller.busy()&&controller.notice().contains("logout denied");}));
    CHECK(signout->isVisible());CHECK(QFile::remove(failLogout));
    // A response started before logout cannot resurrect the old IP or QR.
    CHECK(waitFor([&]{return !controller.updates().busy();}));hold=true;controller.refresh();CHECK(waitFor([&]{return bool(held);}));
    signout->click();beforeSignout=read(calls);confirm->click();confirm->click();CHECK(controller.busy());
    hold=false;CHECK(waitFor([&]{return !controller.busy()&&controller.notice().contains("Signed out");}));
    CHECK(read(calls).mid(beforeSignout.size())=="logout\n");
    CHECK(primary->text()=="Sign in"&&primary->isEnabled());
    CHECK(controller.notice().contains("Tap Sign in"));
    CHECK(!identity->isVisible()&&identity->text().isEmpty()&&!signout->isVisible());
    CHECK(signoutQr->image().isNull()&&!signoutLink->isVisible());
    if(held&&held->state()==QLocalSocket::ConnectedState){held->write("HTTP/1.0 200 OK\r\nContent-Length: 26\r\n\r\n{\"BackendState\":\"Running\"}");held->disconnectFromServer();}
    controller.refresh();spin(100);CHECK(identity->text().isEmpty()&&signoutQr->image().isNull());
    f.setFileName(route);CHECK(f.open(QIODevice::WriteOnly|QIODevice::Truncate));f.close();controller.refresh();
    CHECK(waitFor([&]{return state->text()=="Wi-Fi unavailable";}));
    CHECK(primary->text()=="Sign in"&&!primary->isEnabled());
    CHECK(f.open(QIODevice::WriteOnly|QIODevice::Truncate));f.write("Iface Destination Gateway Flags\nwlan0 00000000 0100000A 0003\n");f.close();controller.refresh();
    CHECK(waitFor([&]{return primary->isEnabled()&&state->text()=="Sign-in required";}));
    CHECK(window.grab().save("build/widget-signed-out.png"));
    // Connect exposes only the new sign-in URL, then normal login restores status.
    const QString retiredLogin=authUrl;authUrl.clear();loggedOutHealth=true;controller.refresh();
    CHECK(waitFor([&]{return !controller.refreshing();}));
    f.setFileName(failConnect);CHECK(f.open(QIODevice::WriteOnly));f.close();
    primary->click();CHECK(primary->text()=="Cancel");CHECK(waitFor([&]{return read(last).trimmed()=="signin";}));controller.refresh();
    CHECK(waitFor([&]{return !controller.busy()&&!controller.refreshing();}));
    CHECK(primary->text()=="Sign in");
    CHECK(signoutQr->image().isNull()&&!signoutLink->isVisible()&&identity->text().isEmpty());
    CHECK(controller.notice().contains("Sign-in request timed out"));
    // A URL can arrive only in a later poll, after the command has timed out.
    authUrl="https://login.tailscale.com/a/late-arrival-123";login=true;controller.refresh();
    CHECK(waitFor([&]{return signoutQr->isVisible()&&signoutLink->text().contains(authUrl);}));
    CHECK(controller.notice().isEmpty());
    // A completed login also retires a connection failure without another tap.
    login=false;loggedOutHealth=false;authUrl.clear();CHECK(QFile::remove(signedOutFile));controller.refresh();
    CHECK(waitFor([&]{return state->text()=="Connected"&&!controller.updates().busy();}));CHECK(controller.notice().isEmpty());
    signout->click();confirm->click();CHECK(waitFor([&]{return !controller.busy()&&controller.notice().contains("Signed out");}));
    primary->click();controller.refresh();CHECK(waitFor([&]{return !controller.busy()&&!controller.refreshing();}));
    CHECK(controller.notice().contains("Sign-in request timed out"));
    CHECK(QFile::remove(signedOutFile));controller.refresh();CHECK(waitFor([&]{return state->text()=="Connected";}));CHECK(controller.notice().isEmpty());
    authUrl=retiredLogin;CHECK(waitFor([&]{return signout->isEnabled();}));signout->click();confirm->click();CHECK(waitFor([&]{return !controller.busy()&&!controller.refreshing()&&controller.notice().contains("Signed out");}));
    CHECK(QFile::remove(failConnect));
    // A fresh poll during slow startup can still contain the retired URL.
    primary->click();controller.refresh();spin(90);
    CHECK(signoutQr->image().isNull()&&!signoutLink->isVisible()&&identity->text().isEmpty());
    authUrl="https://login.tailscale.com/a/fresh-signin-123?next=%2Fbooks";login=true;CHECK(QFile::remove(signedOutFile));controller.refresh();
    CHECK(waitFor([&]{return !controller.busy()&&signoutQr->isVisible()&&signoutLink->text().contains(authUrl);}));CHECK(controller.notice().isEmpty());
    CHECK(primary->text()=="Sign in");
    CHECK(!signout->isVisible());CHECK(window.grab().save("build/widget-signout-login.png"));
    login=false;controller.refresh();CHECK(waitFor([&]{return state->text()=="Connected"&&signout->isVisible()&&signout->isEnabled();}));
    CHECK(!signoutQr->isVisible()&&!signoutLink->isVisible());CHECK(primary->text()=="Disconnect");
    // Expired authentication requires Sign in independently of HaveNodeKey.
    login=true;loginKey=true;controller.refresh();
    CHECK(waitFor([&]{return state->text()=="Sign-in required"&&!controller.refreshing();}));CHECK(primary->text()=="Sign in");
    login=false;loginKey=false;controller.refresh();CHECK(waitFor([&]{return state->text()=="Connected";}));CHECK(primary->text()=="Disconnect");
    // Probe unchanged real LocalAPI polls after the initial layout has settled.
    spin(100);PaintProbe idleProbe(&panel);app.installEventFilter(&idleProbe);
    for(int i=0;i<4;++i){int before=requests;controller.refresh();CHECK(waitFor([&]{return requests>before&&!controller.refreshing();}));spin(60);}
    app.removeEventFilter(&idleProbe);
    std::fprintf(stderr,"Unchanged status polls: %d paints, %d layouts, %d resizes\n",idleProbe.paints,idleProbe.layouts,idleProbe.resizes);
    panel.close();spin(100);PaintProbe openingProbe(&panel);app.installEventFilter(&openingProbe);panel.open();spin(150);app.removeEventFilter(&openingProbe);
    std::fprintf(stderr,"Opening known status: %d panel paints, %d paint frames, %d resizes\n",openingProbe.panelPaints,openingProbe.paintFrames,openingProbe.resizes);
    const QByteArray callsBeforeExpansion=read(updateCalls);
    PaintProbe expansionProbe(&panel);app.installEventFilter(&expansionProbe);updateButton->click();spin(200);app.removeEventFilter(&expansionProbe);
    std::fprintf(stderr,"Opening Updates: %d panel paints, %d paint frames, %d resizes\n",expansionProbe.panelPaints,expansionProbe.paintFrames,expansionProbe.resizes);
    CHECK(read(updateCalls)==callsBeforeExpansion);
    updateButton->click();spin(100);
    // Hardware touch first arrives at QWindow, before widget touch acceptance.
    book.setAttribute(Qt::WA_AcceptTouchEvents,false);
    QTouchDevice nativeDevice;nativeDevice.setType(QTouchDevice::TouchScreen);
    QTouchEvent::TouchPoint nativePoint(4);nativePoint.setPos(book.mapTo(&window,book.rect().center()));nativePoint.setScreenPos(book.mapToGlobal(book.rect().center()));nativePoint.setState(Qt::TouchPointPressed);
    QTouchEvent nativeBegin(QEvent::TouchBegin,&nativeDevice,Qt::NoModifier,Qt::TouchPointPressed,{nativePoint});QApplication::sendEvent(window.windowHandle(),&nativeBegin);
    nativePoint.setState(Qt::TouchPointReleased);QTouchEvent nativeEnd(QEvent::TouchEnd,&nativeDevice,Qt::NoModifier,Qt::TouchPointReleased,{nativePoint});QApplication::sendEvent(window.windowHandle(),&nativeEnd);
    std::fprintf(stderr,"Native-window outside tap dismissed: %d\n",!panel.isVisible());
    CHECK(!panel.isVisible()&&bookClicks==0&&nativeBegin.isAccepted()&&nativeEnd.isAccepted());
    CHECK(expansionProbe.panelPaints<=1&&expansionProbe.paintFrames<=1);
    CHECK(openingProbe.panelPaints==1&&openingProbe.paintFrames==1);
    CHECK(idleProbe.paints==0&&idleProbe.resizes==0);
    mouseTap(&book,Qt::MouseEventSynthesizedByQt);CHECK(bookClicks==0);
    book.setAttribute(Qt::WA_AcceptTouchEvents);panel.open();spin(700);
    // The native handler only owns this window, and leaves inside delivery to Qt.
    QWindow unrelated;unrelated.create();QApplication::sendEvent(&unrelated,&nativeBegin);QApplication::sendEvent(&unrelated,&nativeEnd);CHECK(panel.isVisible());
    CHECK(nativeTouchTap(updateButton));CHECK(panel.findChild<QWidget*>("tailuiUpdateSection")->isVisible());
    CHECK(nativeTouchTap(updateButton));CHECK(!panel.findChild<QWidget*>("tailuiUpdateSection")->isVisible());
    // A touchscreen tap may be followed by a duplicate mouse sequence.
    touchTap(updateButton);CHECK(panel.findChild<QWidget*>("tailuiUpdateSection")->isVisible());
    mouseTap(updateButton,Qt::MouseEventSynthesizedByQt);CHECK(panel.findChild<QWidget*>("tailuiUpdateSection")->isVisible());
    touchTap(updateButton);CHECK(!panel.findChild<QWidget*>("tailuiUpdateSection")->isVisible());
    touchTap(updateButton);CHECK(panel.findChild<QWidget*>("tailuiUpdateSection")->isVisible());
    updateButton->click();spin(30);
    int compactHeight=panel.height();
    CHECK(window.grab().save("build/widget-connected.png"));
    // Clara HD uses the same controls at its own screen size and text scale.
    {
        QWidget claraWindow;claraWindow.resize(1072,1448);claraWindow.setStyleSheet("background:white;");
        QWidget claraAnchor(&claraWindow);claraAnchor.setGeometry(888,20,80,90);claraWindow.show();
        tailui::Panel clara(&claraWindow,&controller,&claraAnchor);clara.open();spin(100);
        CHECK(clara.findChild<QLabel*>("tailuiState")->text()=="Connected");
        CHECK(claraWindow.rect().adjusted(10,10,-10,-10).contains(clara.geometry()));
        CHECK(clara.grab().save("build/clara-connected.png"));
        clara.findChild<QPushButton*>("tailuiUpdates")->click();spin(100);
        CHECK(claraWindow.rect().adjusted(10,10,-10,-10).contains(clara.geometry()));
        CHECK(clara.findChild<QScrollArea*>()->horizontalScrollBar()->maximum()==0);
        CHECK(clara.grab().save("build/clara-updates.png"));
    }
    window.activateWindow();panel.open();spin(80);
    const QImage disconnectIcon=primary->icon().pixmap(primary->iconSize()).toImage();
    primary->setDown(true);CHECK(centerPixels(primary->grab().toImage(),primary->iconSize().width()+4,230,255)>0);primary->setDown(false);
    primary->click();CHECK(waitFor([&]{return state->text()=="Disconnected";}));
    CHECK(primary->text()=="Connect"&&primary->icon().pixmap(primary->iconSize()).toImage()!=disconnectIcon);
    CHECK(identity->text()=="<b>reader</b>");
    primary->click();CHECK(!identity->text().contains("100.64.0.8"));
    CHECK(waitFor([&]{return !controller.busy()&&state->text()=="Connected"&&identity->text().contains("100.64.0.8");}));
    primary->click();CHECK(waitFor([&]{return !controller.busy()&&state->text()=="Disconnected";}));CHECK(identity->text()=="<b>reader</b>");
    CHECK(!sawPeers);spin(30);compactHeight=panel.height();
    // Show update controls in place, retaining the connection action and title.
    updateButton->click();auto section=panel.findChild<QWidget*>("tailuiUpdateSection");
    auto versions=panel.findChild<QLabel*>("tailuiUpdateVersions");auto installTs=panel.findChild<QPushButton*>("tailuiInstallTailscale");
    CHECK(waitFor([&]{return section->isVisible()&&versions->text().contains("1.104.6")&&installTs->isEnabled();}));
    CHECK(primary->isVisible());CHECK(waitFor([&]{return panel.height()>compactHeight;}));
    CHECK(window.grab().save("build/widget-expanded.png"));
    CHECK(!read(updateCalls).contains("install"));
    const QByteArray normalUpdates=read(updateStatus);
    auto writeStatus=[&](const QByteArray &value){QFile statusFile(updateStatus);CHECK(statusFile.open(QIODevice::WriteOnly|QIODevice::Truncate));CHECK(statusFile.write(value)==value.size());statusFile.close();controller.updates().load();};
    auto scrollArea=panel.findChild<QScrollArea*>();
    QJsonObject longUpdates=QJsonDocument::fromJson(normalUpdates).object();longUpdates.insert("reason",QString(2500,'a'));
    writeStatus(QJsonDocument(longUpdates).toJson());CHECK(waitFor([&]{return !controller.updates().busy()&&scrollArea->verticalScrollBar()->maximum()>200;}));
    int installClicks=0;QObject::connect(installTs,&QPushButton::clicked,[&]{++installClicks;});
    touchSwipe(installTs,-180);CHECK(installClicks==0&&!controller.busy());CHECK(scrollArea->verticalScrollBar()->value()>=170);
    // Restart must also remain an explicit tap, even while updates can scroll.
    longUpdates.insert("restart",true);writeStatus(QJsonDocument(longUpdates).toJson());
    auto restart=panel.findChild<QPushButton*>("tailuiRestart");CHECK(waitFor([&]{return restart->isVisible()&&!controller.updates().busy();}));
    scrollArea->verticalScrollBar()->setValue(0);spin(30);int restartClicks=0;QObject::connect(restart,&QPushButton::clicked,[&]{++restartClicks;});
    touchSwipe(restart,-180);CHECK(restartClicks==0&&!controller.busy());
    writeStatus(normalUpdates);CHECK(waitFor([&]{return !controller.updates().busy()&&installTs->isVisible()&&!restart->isVisible();}));scrollArea->verticalScrollBar()->setValue(0);spin(30);
    auto automatic=panel.findChild<QPushButton*>("tailuiAutomaticChecks");automatic->click();
    CHECK(waitFor([&]{return automatic->text().contains("Off")&&!controller.updates().busy();}));CHECK(installTs->isEnabled());
    automatic->click();CHECK(waitFor([&]{return automatic->text().contains("On")&&!controller.updates().busy();}));
    panel.findChild<QPushButton*>("tailuiCheckUpdates")->click();CHECK(waitFor([&]{return !controller.updates().busy();}));
    updateButton->click();CHECK(!section->isVisible());CHECK(waitFor([&]{return panel.height()==compactHeight;}));
    updateButton->click();CHECK(waitFor([&]{return section->isVisible()&&!controller.updates().busy();}));
    installTs->click();CHECK(controller.busy());panel.close();CHECK(controller.busy());CHECK(waitFor([&]{return !controller.busy();}));
    CHECK(read(last).trimmed()=="install"&&controller.notice().contains("Tailscale update installed"));
    panel.open();CHECK(!section->isVisible());
    // Finish the Qt 5.2 fallback mouse-suppression window after button touches.
    spin(700);
    // Outside mouse dismissal must not activate the library underneath.
    mouseTap(&book);CHECK(!panel.isVisible()&&bookClicks==0);
    mouseTap(&book);CHECK(bookClicks==1);
    panel.open();touchTap(&book);CHECK(!panel.isVisible()&&bookClicks==1);
    mouseTap(&book,Qt::MouseEventSynthesizedByQt);CHECK(bookClicks==1);spin(700);
    mouseTap(&book);CHECK(bookClicks==2);
    panel.open();anchor.hide();CHECK(waitFor([&]{return !panel.isVisible();}));anchor.show();panel.open();
    QWidget differentParent(&window);anchor.setParent(&differentParent);CHECK(!panel.isVisible());anchor.setParent(&window);anchor.setGeometry(1080,20,80,90);anchor.show();panel.open();
    QWidget other(nullptr,Qt::Popup);other.resize(100,100);other.show();CHECK(waitFor([&]{return !panel.isVisible();}));other.close();
    window.activateWindow();spin(30);panel.open();spin(30);CHECK(panel.isVisible());
    // Errors use literal text and status automatically recovers on the next poll.
    malformed=true;controller.refresh();CHECK(waitFor([&]{return state->text()=="Status unavailable";}));malformed=false;
    controller.refresh();CHECK(waitFor([&]{return state->text()=="Connected";}));
    approval=true;controller.refresh();CHECK(waitFor([&]{return state->text()=="Sign-in required";}));CHECK(primary->text()=="Connect");
    touchTap(primary);CHECK(primary->text()=="Cancel"&&controller.connecting());
    mouseTap(primary,Qt::MouseEventSynthesizedByQt);CHECK(controller.connecting());CHECK(waitFor([&]{return !controller.busy();}));
    CHECK(!panel.findChild<QLabel*>("tailuiNotice")->text().contains("link below"));
    CHECK(panel.findChild<QLabel*>("tailuiNotice")->text().contains("admin console"));
    // Authentication guidance expires with the status that generated it.
    approval=false;controller.refresh();CHECK(waitFor([&]{return state->text()=="Connected";}));
    CHECK(!panel.findChild<QLabel*>("tailuiNotice")->text().contains("admin console"));
    approval=true;controller.refresh();CHECK(waitFor([&]{return state->text()=="Sign-in required";}));primary->click();CHECK(waitFor([&]{return !controller.busy();}));
    CHECK(panel.findChild<QLabel*>("tailuiNotice")->text().contains("admin console"));
    CHECK(!panel.findChild<QWidget*>("tailuiLoginQr")->isVisible());
    // A short current URL appears both as a complete link and a scannable QR.
    approval=false;login=true;controller.refresh();CHECK(waitFor([&]{return panel.findChild<QLabel*>("tailuiLogin")->isVisible();}));
    auto loginLabel=panel.findChild<QLabel*>("tailuiLogin");
    auto qrBase=panel.findChild<QWidget*>("tailuiLoginQr");CHECK(qrBase&&qrBase->isVisible());
    auto qr=static_cast<tailui::QrWidget*>(qrBase);
    CHECK(loginLabel->text().contains(authUrl)&&!qr->image().isNull());
    CHECK(!panel.findChild<QLabel*>("tailuiNotice")->text().contains("admin console"));
    CHECK(waitFor([&]{return panel.height()>qr->height();}));
    CHECK(qr->mapTo(&panel,QPoint()).y()<loginLabel->mapTo(&panel,QPoint()).y());
    CHECK(window.rect().adjusted(10,10,-10,-10).contains(panel.geometry()));
    CHECK(window.grab().save("build/widget-login-qr.png"));
    {
        QWidget claraWindow;claraWindow.resize(1072,1448);claraWindow.setStyleSheet("background:white;");
        QWidget claraAnchor(&claraWindow);claraAnchor.setGeometry(888,20,80,90);claraWindow.show();
        tailui::Panel clara(&claraWindow,&controller,&claraAnchor);clara.open();spin(100);
        auto claraQr=clara.findChild<tailui::QrWidget*>("tailuiLoginQr");
        auto claraScroll=clara.findChild<QScrollArea*>();
        CHECK(claraQr->isVisible()&&!claraQr->image().isNull());
        CHECK(claraWindow.rect().adjusted(10,10,-10,-10).contains(clara.geometry()));
        claraScroll->ensureWidgetVisible(claraQr,0,0);spin(40);
        CHECK(claraScroll->viewport()->rect().contains(QRect(claraQr->mapTo(claraScroll->viewport(),QPoint()),claraQr->size())));
        CHECK(clara.grab().save("build/clara-login-qr.png"));
    }
    window.activateWindow();panel.open();spin(80);
    const qint64 qrKey=qr->image().cacheKey();spin(100);PaintProbe loginIdle(&panel);app.installEventFilter(&loginIdle);
    int beforeLoginPoll=requests;controller.refresh();CHECK(waitFor([&]{return requests>beforeLoginPoll&&!controller.refreshing();}));spin(60);app.removeEventFilter(&loginIdle);
    CHECK(qr->image().cacheKey()==qrKey&&loginIdle.paints==0&&loginIdle.resizes==0);
    // Scrollbars must never crop the complete QR rectangle or its quiet zone.
    window.resize(514,700);anchor.move(350,20);spin(100);
    auto narrowScroll=panel.findChild<QScrollArea*>();CHECK(narrowScroll->verticalScrollBar()->maximum()>0);
    narrowScroll->ensureWidgetVisible(qr,0,0);spin(80);
    CHECK(narrowScroll->viewport()->rect().contains(QRect(qr->mapTo(narrowScroll->viewport(),QPoint()),qr->size())));
    const qint64 narrowKey=qr->image().cacheKey();PaintProbe narrowIdle(&panel);app.installEventFilter(&narrowIdle);
    beforeLoginPoll=requests;controller.refresh();CHECK(waitFor([&]{return requests>beforeLoginPoll&&!controller.refreshing();}));spin(80);app.removeEventFilter(&narrowIdle);
    CHECK(qr->image().cacheKey()==narrowKey&&narrowIdle.paints==0&&narrowIdle.resizes==0);
    CHECK(window.grab().save("build/widget-login-qr-narrow.png"));
    window.resize(1264,1680);anchor.move(1080,20);spin(80);
    const qint64 beforeChangedUrl=qr->image().cacheKey();
    authUrl="https://login.tailscale.com/a/replacement?next=%2Fbooks&token=A%2Bb%26C";controller.refresh();
    CHECK(waitFor([&]{return loginLabel->text().contains(authUrl)&&qr->image().cacheKey()!=beforeChangedUrl;}));
    CHECK(qr->grab().save("build/qr-panel.png"));QFile qrPayload("build/qr-panel.txt");CHECK(qrPayload.open(QIODevice::WriteOnly));CHECK(qrPayload.write(authUrl.toUtf8())==authUrl.toUtf8().size());qrPayload.close();
    // A current sign-in link remains usable while the Kobo's Wi-Fi is down.
    f.setFileName(route);CHECK(f.open(QIODevice::WriteOnly|QIODevice::Truncate));f.close();controller.refresh();CHECK(waitFor([&]{return state->text()=="Wi-Fi unavailable";}));CHECK(qr->isVisible());
    CHECK(f.open(QIODevice::WriteOnly|QIODevice::Truncate));f.write("Iface Destination Gateway Flags\nwlan0 00000000 0100000A 0003\n");f.close();controller.refresh();CHECK(waitFor([&]{return state->text()=="Sign-in required";}));
    window.resize(1680,1264);anchor.move(1500,20);spin(80);CHECK(window.rect().adjusted(10,10,-10,-10).contains(panel.geometry()));CHECK(qr->width()<=420);CHECK(window.grab().save("build/widget-login-qr-landscape.png"));
    window.resize(1264,1680);anchor.move(1080,20);spin(80);
    // A long, unbroken login URL remains complete when no readable QR will fit.
    authUrl="https://login.tailscale.com/a/"+QString(2500,'a');controller.refresh();CHECK(waitFor([&]{return loginLabel->text().contains(authUrl);}));
    CHECK(!qr->isVisible()&&qr->image().isNull());CHECK(panel.findChild<QLabel*>("tailuiLoginCaption")->text().contains("link below"));
    CHECK(waitFor([&]{return loginLabel->height()>loginLabel->fontMetrics().height();}));
    CHECK(window.rect().adjusted(10,10,-10,-10).contains(panel.geometry()));
    CHECK(panel.findChild<QScrollArea*>()->horizontalScrollBar()->maximum()==0);
    CHECK(window.grab().save("build/widget-login.png"));
    auto scroll=panel.findChild<QScrollArea*>();CHECK(scroll->verticalScrollBar()->maximum()>0);
    QTouchDevice scrollDevice;scrollDevice.setType(QTouchDevice::TouchScreen);
    QTouchEvent::TouchPoint drag(0);drag.setPos(scroll->viewport()->rect().center());drag.setScreenPos(scroll->viewport()->mapToGlobal(drag.pos().toPoint()));drag.setState(Qt::TouchPointPressed);
    QTouchEvent dragBegin(QEvent::TouchBegin,&scrollDevice,Qt::NoModifier,Qt::TouchPointPressed,{drag});QApplication::sendEvent(scroll->viewport(),&dragBegin);
    drag.setPos(drag.pos()-QPointF(0,180));drag.setScreenPos(drag.screenPos()-QPointF(0,180));drag.setState(Qt::TouchPointMoved);
    QTouchEvent dragUpdate(QEvent::TouchUpdate,&scrollDevice,Qt::NoModifier,Qt::TouchPointMoved,{drag});QApplication::sendEvent(scroll->viewport(),&dragUpdate);
    CHECK(scroll->verticalScrollBar()->value()>=170);
    drag.setState(Qt::TouchPointReleased);QTouchEvent dragEnd(QEvent::TouchEnd,&scrollDevice,Qt::NoModifier,Qt::TouchPointReleased,{drag});QApplication::sendEvent(scroll->viewport(),&dragEnd);
    // LocalAPI may retain AuthURL after sign-in; neither link nor QR should return.
    login=false;staleAuth=true;controller.refresh();CHECK(waitFor([&]{return !loginLabel->isVisible();}));CHECK(qr->image().isNull()&&!qr->isVisible());
    // Setup keeps retry inside the small widget.
    f.setFileName(failPrepare);CHECK(f.open(QIODevice::WriteOnly));f.close();
    CHECK(controller.prepareRuntime());CHECK(!primary->isEnabled());CHECK(waitFor([&]{return !controller.busy();}));
    CHECK(!controller.runtimeReady()&&primary->text()=="Retry setup"&&primary->isEnabled());
    CHECK(QFile::remove(failPrepare));primary->click();CHECK(waitFor([&]{return controller.runtimeReady()&&!controller.busy();}));
    updateButton->click();CHECK(waitFor([&]{return section->isVisible()&&!controller.updates().busy();}));
    window.resize(1680,1264);anchor.move(1500,20);spin(80);
    CHECK(window.rect().adjusted(10,10,-10,-10).contains(panel.geometry()));
    CHECK(panel.width()<window.width());CHECK(window.grab().save("build/widget-landscape.png"));
    f.setFileName(route);CHECK(f.open(QIODevice::WriteOnly|QIODevice::Truncate));f.close();controller.refresh();
    CHECK(waitFor([&]{return !installTs->isEnabled();}));CHECK(!panel.findChild<QPushButton*>("tailuiCheckUpdates")->isEnabled());CHECK(identity->text()=="<b>reader</b>");CHECK(!loginLabel->isVisible()&&!qr->isVisible());
    panel.close();CHECK(waitFor([&]{return !controller.refreshing();}));spin(100);int afterClose=requests;spin(3200);CHECK(requests==afterClose);
    CHECK(!sawPeers);
    auto dyingAnchor=new QWidget(&window);dyingAnchor->setGeometry(1500,20,80,90);dyingAnchor->show();
    tailui::Panel detached(&window,&controller,dyingAnchor);detached.open();delete dyingAnchor;CHECK(!detached.isVisible());
    auto temporaryController=new tailui::Controller(dir.path()+"/socket",helper,route,nullptr,updater);
    tailui::Panel orphan(&window,temporaryController,&anchor);orphan.open();delete temporaryController;CHECK(!orphan.isVisible());
    std::puts("Compact title, in-place Updates, actions, long text, dismissal, bounds and lifecycle tests passed.");
}
