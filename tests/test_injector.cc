#include <QApplication>
#include <QBoxLayout>
#include <QEvent>
#include <QFile>
#include <QLabel>
#include <QImage>
#include <QMouseEvent>
#include <QPushButton>
#include <QTemporaryDir>
#include <QTouchDevice>
#include <QTouchEvent>
#include <cstdio>
#include <cstdlib>
#include "injector.h"
#include "controller.h"

#define CHECK(x) do { if (!(x)) { std::fprintf(stderr,"FAIL line %d: %s\n",__LINE__,#x); std::exit(1); } } while(false)
class StatusBarView : public QWidget { Q_OBJECT public: using QWidget::QWidget; };
static QWidget *rowFor(StatusBarView *bar){
    auto layout=new QVBoxLayout(bar);auto row=new QWidget(bar);row->setObjectName("iconContainer");layout->addWidget(row);
    auto icons=new QHBoxLayout(row);auto search=new QWidget(row);search->setObjectName("searchIcon");search->setFixedSize(80,90);icons->addWidget(search);return row;
}
int main(int argc,char **argv){
    QApplication app(argc,argv);QTemporaryDir dir;CHECK(dir.isValid());
    const QString route=dir.path()+"/route";QFile file(route);CHECK(file.open(QIODevice::WriteOnly));file.write("wlan0 00000000 0100000A 0003\n");file.close();
    tailui::Controller controller(dir.path()+"/missing",dir.path()+"/helper",route);
    QWidget window;window.resize(600,800);auto layout=new QVBoxLayout(&window);auto bar=new StatusBarView(&window);layout->addWidget(bar);layout->addStretch();QWidget *row=rowFor(bar);
    StatusBarView incomplete;incomplete.resize(300,100);
    tailui::Injector injector(&controller);app.installEventFilter(&injector);window.show();incomplete.show();QApplication::processEvents();injector.scanExisting();
    CHECK(row->findChildren<QWidget*>("tailuiStatusIcon").size()==1);
    CHECK(incomplete.findChildren<QWidget*>("tailuiStatusIcon").isEmpty());
    injector.scanExisting();QEvent show(QEvent::Show);QApplication::sendEvent(bar,&show);
    CHECK(row->findChildren<QWidget*>("tailuiStatusIcon").size()==1);
    QWidget *lateRow=rowFor(&incomplete);injector.scanExisting();CHECK(lateRow->findChildren<QWidget*>("tailuiStatusIcon").size()==1);
    QWidget *icon=row->findChild<QWidget*>("tailuiStatusIcon");
    const QImage rendered=icon->grab().toImage();
    for(int y=0;y<rendered.height();++y)CHECK(qGray(rendered.pixel(0,y))>=128&&qGray(rendered.pixel(rendered.width()-1,y))>=128);
    CHECK(window.grab().save("build/toolbar.png"));
    QTouchDevice device;device.setType(QTouchDevice::TouchScreen);
    QTouchEvent::TouchPoint point(0);point.setPos(icon->rect().center());point.setScreenPos(icon->mapToGlobal(icon->rect().center()));point.setState(Qt::TouchPointPressed);
    QTouchEvent begin(QEvent::TouchBegin,&device,Qt::NoModifier,Qt::TouchPointPressed,{point});QApplication::sendEvent(icon,&begin);
    point.setState(Qt::TouchPointReleased);
    QTouchEvent end(QEvent::TouchEnd,&device,Qt::NoModifier,Qt::TouchPointReleased,{point});QApplication::sendEvent(icon,&end);QApplication::processEvents();
    QWidget *panel=window.findChild<QWidget*>("tailuiPanel");
    CHECK(panel&&panel->isVisible());
    CHECK(panel->findChild<QPushButton*>("tailuiPrimary"));
    // Qt may synthesize a mouse sequence after accepted touchscreen input.
    QPointF local=icon->rect().center(),global=icon->mapToGlobal(local.toPoint()),windowPos=icon->mapTo(&window,local.toPoint());
    QMouseEvent press(QEvent::MouseButtonPress,local,windowPos,global,Qt::LeftButton,Qt::LeftButton,Qt::NoModifier,Qt::MouseEventSynthesizedByQt);
    QMouseEvent release(QEvent::MouseButtonRelease,local,windowPos,global,Qt::LeftButton,Qt::NoButton,Qt::NoModifier,Qt::MouseEventSynthesizedByQt);
    QApplication::sendEvent(icon,&press);QApplication::sendEvent(icon,&release);CHECK(panel->isVisible());
    point.setState(Qt::TouchPointPressed);QTouchEvent again(QEvent::TouchBegin,&device,Qt::NoModifier,Qt::TouchPointPressed,{point});QApplication::sendEvent(icon,&again);
    point.setState(Qt::TouchPointReleased);QTouchEvent finish(QEvent::TouchEnd,&device,Qt::NoModifier,Qt::TouchPointReleased,{point});QApplication::sendEvent(icon,&finish);CHECK(!panel->isVisible());
    QApplication::sendEvent(icon,&again);QApplication::sendEvent(icon,&finish);CHECK(panel->isVisible());
    icon->hide();CHECK(!panel->isVisible());icon->show();
    // A tap on another toolbar control dismisses and still reaches that control.
    QPushButton wifi("Wi-Fi",row);wifi.setGeometry(0,0,50,50);wifi.show();int wifiClicks=0;QObject::connect(&wifi,&QPushButton::clicked,[&]{++wifiClicks;});
    QApplication::sendEvent(icon,&again);QApplication::sendEvent(icon,&finish);CHECK(panel->isVisible());
    QPointF wifiLocal=wifi.rect().center(),wifiGlobal=wifi.mapToGlobal(wifiLocal.toPoint()),wifiWindow=wifi.mapTo(&window,wifiLocal.toPoint());
    QMouseEvent wifiPress(QEvent::MouseButtonPress,wifiLocal,wifiWindow,wifiGlobal,Qt::LeftButton,Qt::LeftButton,Qt::NoModifier);
    QMouseEvent wifiRelease(QEvent::MouseButtonRelease,wifiLocal,wifiWindow,wifiGlobal,Qt::LeftButton,Qt::NoButton,Qt::NoModifier);
    QApplication::sendEvent(&wifi,&wifiPress);QApplication::sendEvent(&wifi,&wifiRelease);CHECK(!panel->isVisible()&&wifiClicks==1);
    std::puts("Toolbar insertion, duplicate prevention, layout retry and touch opening tests passed.");
}
#include "test_injector.moc"
