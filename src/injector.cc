// Measured placement and touch routing adapted from Relio, Apache-2.0.
#include "injector.h"
#include "panel.h"
#include "status_glyph.h"
#include <QElapsedTimer>
#include <QTouchEvent>
#include "log.h"
#include <QApplication>
#include <QBoxLayout>
#include <QEvent>
#include <QHideEvent>
#include <QImage>
#include <QMouseEvent>
#include <QPainter>
#include <QShowEvent>
#include <QTimer>
#include <QVariant>

namespace tailui {
namespace {
QRect glyphBounds(QWidget *anchor) {
    const QImage image=anchor->grab().toImage();QRect bounds;
    for(int y=0;y<image.height();++y)for(int x=0;x<image.width();++x)
        if(qGray(image.pixel(x,y))<128)bounds|=QRect(x,y,1,1);
    return bounds;
}

class StatusIcon : public QWidget {
public:
    StatusIcon(QWidget *row,QWidget *window,QWidget *anchor,Controller *controller)
        : QWidget(row),window_(window),anchor_(anchor),controller_(controller) {
        setObjectName("tailuiStatusIcon");setSizePolicy(QSizePolicy::Fixed,QSizePolicy::Expanding);setAttribute(Qt::WA_AcceptTouchEvents);
        measure_.setSingleShot(true);
        QObject::connect(&measure_,&QTimer::timeout,this,[this]{if(anchor_&&anchor_->isVisible()){glyph_=glyphBounds(anchor_);update();}});
        controller_->observe(this,[this]{refreshAppearance();});refreshAppearance();
    }
    QSize sizeHint() const override {
        int side=anchor_?anchor_->sizeHint().height():0;
        if(side<=0)side=anchor_?anchor_->height():70;
        return QSize(qMax(side,40),qMax(side,40));
    }
protected:
    void showEvent(QShowEvent *event) override {
        QWidget::showEvent(event);if(controller_)controller_->setBarVisible(this,true);measure_.start(0);
    }
    void hideEvent(QHideEvent *event) override {
        if(controller_)controller_->setBarVisible(this,false);
        QWidget::hideEvent(event);
    }
    void resizeEvent(QResizeEvent *) override {glyph_=QRect();measure_.start(0);}
    bool event(QEvent *event) override {
        if(event->type()==QEvent::TouchBegin){touching_=true;event->accept();return true;}
        if(event->type()==QEvent::TouchUpdate&&touching_){event->accept();return true;}
        if(event->type()==QEvent::TouchEnd){
            const auto points=static_cast<QTouchEvent*>(event)->touchPoints();
            if(touching_&&!points.isEmpty()&&rect().contains(points.first().pos().toPoint()))openPanel();
            touching_=false;lastTouch_.start();event->accept();return true;
        }
        if(event->type()==QEvent::TouchCancel){touching_=false;lastTouch_.start();event->accept();return true;}
        return QWidget::event(event);
    }
    void mousePressEvent(QMouseEvent *event) override { event->accept(); }
    void mouseReleaseEvent(QMouseEvent *event) override {
        if(lastTouch_.isValid()&&lastTouch_.elapsed()<600){event->accept();return;}
        if(event->button()==Qt::LeftButton&&rect().contains(event->pos()))openPanel();
    }
    void paintEvent(QPaintEvent *) override {
        const qreal measured=glyph_.isValid()?glyph_.height():height()*0.4;
        const qreal side=qMin(measured,qMin(width(),height())*0.85);
        const qreal anchorY=glyph_.isValid()?glyph_.center().y()+0.5:height()/2.0;
        const qreal cy=qBound(side/2,anchorY,height()-side/2);
        QPainter painter(this);painter.setRenderHint(QPainter::Antialiasing);
        painter.translate((width()-side)/2,cy-side/2);painter.scale(side/42,side/42);
        paintStatusGlyph(painter,connected_,attention_);
    }
private:
    void openPanel(){
        if(!controller_)return;
        QWidget *actualWindow=window();
        if(panel_&&panel_->parentWidget()!=actualWindow){delete panel_;}
        if(!panel_)panel_=new Panel(actualWindow,controller_,this);
        panel_->toggle();
    }
    void refreshAppearance(){
        if(!controller_)return;
        const Status status=controller_->status();
        const bool connected=status.state==State::Connected&&controller_->runtimeReady()&&!controller_->preparing();
        const bool attention=status.state==State::Error||status.state==State::NoWifi||status.state==State::NeedsLogin||status.state==State::Connecting||!status.health.isEmpty()||!controller_->runtimeReady()||controller_->preparing();
        if(connected_!=connected||attention_!=attention){connected_=connected;attention_=attention;update();}
    }
    QPointer<QWidget> window_,anchor_;
    QPointer<Controller> controller_;
    QPointer<Panel> panel_;
    QTimer measure_;
    QRect glyph_;
    QElapsedTimer lastTouch_;
    bool connected_=false,attention_=false,touching_=false;
};
}

void Injector::scanExisting(){
    if(!controller_)return;
    for(QWidget *widget:QApplication::allWidgets())if(widget->inherits("StatusBarView"))inject(widget);
}
bool Injector::eventFilter(QObject *watched,QEvent *event){
    if(event->type()==QEvent::Show&&watched->isWidgetType()&&watched->inherits("StatusBarView"))inject(static_cast<QWidget*>(watched));
    return false;
}
void Injector::inject(QWidget *bar){
    if(!controller_||bar->property("tailuiInjected").toBool())return;
    QWidget *row=bar->findChild<QWidget*>("iconContainer");
    QBoxLayout *layout=row?qobject_cast<QBoxLayout*>(row->layout()):nullptr;
    QWidget *anchor=row?row->findChild<QWidget*>("searchIcon",Qt::FindDirectChildrenOnly):nullptr;
    if(!layout||!anchor||layout->indexOf(anchor)<0){log("Compatible status-bar layout not found; skipping this view.");return;}
    if(row->findChild<QWidget*>("tailuiStatusIcon",Qt::FindDirectChildrenOnly)){bar->setProperty("tailuiInjected",true);return;}
    StatusIcon *icon=new StatusIcon(row,bar->window(),anchor,controller_);
    layout->insertWidget(layout->indexOf(anchor),icon);bar->setProperty("tailuiInjected",true);icon->show();
    log("Tailscale icon inserted beside Search.");
}
} // namespace tailui
