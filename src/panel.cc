#include "panel.h"
#include "popup_geometry.h"
#include "touch_button.h"
#include "qr_widget.h"
#include <QApplication>
#include <QDateTime>
#include <QHideEvent>
#include <QLabel>
#include <QMouseEvent>
#include <QPainter>
#include <QScrollArea>
#include <QScrollBar>
#include <QShowEvent>
#include <QStyle>
#include <QTouchEvent>
#include <QVBoxLayout>
#include <QWindow>

namespace tailui {
namespace {
// QLabel's default minimum width can retain an unbroken URL. Use the same
// arbitrary wrapping for measurement and painting without changing its text.
class WrappingLabel : public QLabel {
public:
    explicit WrappingLabel(QWidget *parent) : QLabel(parent) {
        setTextFormat(Qt::PlainText); setWordWrap(true);
        QSizePolicy policy(QSizePolicy::Preferred,QSizePolicy::Preferred);
        policy.setHeightForWidth(true);setSizePolicy(policy);
    }
    QSize minimumSizeHint() const override { return QSize(0,fontMetrics().height()); }
    QSize sizeHint() const override { return QSize(600,heightForWidth(600)); }
    int heightForWidth(int width) const override {
        return fontMetrics().boundingRect(QRect(0,0,qMax(1,width),100000),
            Qt::TextWordWrap|Qt::TextWrapAnywhere,text()).height();
    }
protected:
    void paintEvent(QPaintEvent *) override {
        QPainter p(this);p.setPen(palette().color(QPalette::WindowText));
        p.drawText(rect(),Qt::AlignLeft|Qt::AlignTop|Qt::TextWordWrap|Qt::TextWrapAnywhere,text());
    }
};
QLabel *label(QWidget *parent,const QString &name) {
    QLabel *out=new WrappingLabel(parent);out->setObjectName(name);return out;
}
TouchButton *button(QWidget *parent,const QString &name,const QString &text,ButtonGlyph glyph,int unit) {
    TouchButton *out=new TouchButton(text,parent);out->setObjectName(name);out->ensurePolished();
    const int edge=qMax(18,unit*4/3);out->setIconSize(buttonIconSize(edge));out->setGlyph(glyph);return out;
}
void text(QLabel *label,const QString &value) { if(label->text()!=value)label->setText(value); }
}

Panel::Panel(QWidget *window,Controller *controller,QWidget *anchor)
    : QWidget(window),controller_(controller),anchor_(anchor),unit_(qMax(18,window->height()/64)),arrow_(unit_/2) {
    setObjectName("tailuiPanel");setAttribute(Qt::WA_AcceptTouchEvents);hide();
    setStyleSheet(QString("QWidget#tailuiContent { background: white; color: black; } QLabel { color: black; background: white; font-size: %1px; } QPushButton { color: black; background: #ddd; border: 1px solid #999; border-radius: 0px; font-size: %1px; padding: 12px 16px; min-height: 26px; } QPushButton:pressed { background: black; color: white; } QPushButton:disabled { color: #777; border-color: #bbb; background: #eee; }").arg(unit_));
    scroll_=new QScrollArea(this);scroll_->setFrameShape(QFrame::NoFrame);scroll_->setWidgetResizable(false);
    scroll_->setHorizontalScrollBarPolicy(Qt::ScrollBarAlwaysOff);scroll_->setStyleSheet("QScrollArea { background: white; }");
    scroll_->viewport()->setAttribute(Qt::WA_AcceptTouchEvents);
    content_=new QWidget;content_->setObjectName("tailuiContent");content_->setAttribute(Qt::WA_AcceptTouchEvents);
    column_=new QVBoxLayout(content_);column_->setSizeConstraint(QLayout::SetNoConstraint);
    column_->setContentsMargins(0,0,0,0);column_->setSpacing(unit_);
    QLabel *title=label(content_,"tailuiTitle");title->setText("K-Tailscale");title->setStyleSheet("font-weight: bold;");column_->addWidget(title);
    state_=label(content_,"tailuiState");state_->setStyleSheet("font-weight: bold;");column_->addWidget(state_);
    identity_=label(content_,"tailuiIdentity");column_->addWidget(identity_);
    notice_=label(content_,"tailuiNotice");column_->addWidget(notice_);
    loginCaption_=label(content_,"tailuiLoginCaption");column_->addWidget(loginCaption_);
    loginQr_=new QrWidget(content_);loginQr_->setObjectName("tailuiLoginQr");column_->addWidget(loginQr_,0,Qt::AlignHCenter);
    login_=label(content_,"tailuiLogin");column_->addWidget(login_);
    QHBoxLayout *actions=new QHBoxLayout;actions->setSpacing(unit_);
    primary_=button(content_,"tailuiPrimary","Connect",ButtonGlyph::Connect,unit_);actions->addWidget(primary_,1);
    updatesButton_=button(content_,"tailuiUpdates","Updates",ButtonGlyph::Down,unit_);actions->addWidget(updatesButton_,1);column_->addLayout(actions);
    signOut_=button(content_,"tailuiSignOut","Sign out",ButtonGlyph::SignOut,unit_);column_->addWidget(signOut_);
    signOutSection_=new QWidget(content_);signOutSection_->setObjectName("tailuiSignOutSection");
    QVBoxLayout *confirmation=new QVBoxLayout(signOutSection_);confirmation->setContentsMargins(0,0,0,0);confirmation->setSpacing(unit_);
    QLabel *question=new QLabel(signOutSection_);question->setObjectName("tailuiSignOutQuestion");question->setTextFormat(Qt::PlainText);question->setWordWrap(true);
    question->setText("Sign out of Tailscale? You’ll need to sign in again to connect.");confirmation->addWidget(question);
    QHBoxLayout *confirmActions=new QHBoxLayout;confirmActions->setSpacing(unit_);
    signOutCancel_=button(signOutSection_,"tailuiSignOutCancel","Cancel",ButtonGlyph::Close,unit_);confirmActions->addWidget(signOutCancel_,1);
    signOutConfirm_=button(signOutSection_,"tailuiSignOutConfirm","Sign out",ButtonGlyph::SignOut,unit_);confirmActions->addWidget(signOutConfirm_,1);
    confirmation->addLayout(confirmActions);column_->addWidget(signOutSection_);signOutSection_->hide();
    updateSection_=new QWidget(content_);updateSection_->setObjectName("tailuiUpdateSection");
    QVBoxLayout *cards=new QVBoxLayout(updateSection_);cards->setContentsMargins(0,unit_,0,0);cards->setSpacing(unit_);
    updateVersions_=label(updateSection_,"tailuiUpdateVersions");cards->addWidget(updateVersions_);
    installTailscale_=button(updateSection_,"tailuiInstallTailscale","Install Tailscale update",ButtonGlyph::Down,unit_);cards->addWidget(installTailscale_);
    addonVersions_=label(updateSection_,"tailuiAddonVersions");cards->addWidget(addonVersions_);
    installAddon_=button(updateSection_,"tailuiInstallAddon","Install addon update",ButtonGlyph::Down,unit_);cards->addWidget(installAddon_);
    restart_=button(updateSection_,"tailuiRestart","Restart Kobo",ButtonGlyph::Refresh,unit_);cards->addWidget(restart_);
    QHBoxLayout *checks=new QHBoxLayout;checks->setSpacing(unit_);
    checkUpdates_=button(updateSection_,"tailuiCheckUpdates","Check now",ButtonGlyph::Refresh,unit_);checks->addWidget(checkUpdates_);
    automaticChecks_=button(updateSection_,"tailuiAutomaticChecks","Checks: On",ButtonGlyph::Refresh,unit_);checks->addWidget(automaticChecks_);cards->addLayout(checks);
    updateNotice_=label(updateSection_,"tailuiUpdateNotice");cards->addWidget(updateNotice_);column_->addWidget(updateSection_);updateSection_->hide();
    scroll_->setWidget(content_);
    QObject::connect(updatesButton_,&QPushButton::clicked,this,[this]{
        confirmingSignOut_=false;confirmationContext_.clear();
        updatesExpanded_=!updatesExpanded_;updateSection_->setVisible(updatesExpanded_);
        updatesButton_->setGlyph(updatesExpanded_?ButtonGlyph::Up:ButtonGlyph::Down);
        // Initial/background checks and completed actions keep this cache fresh.
        // Re-reading it on every expansion creates a needless busy/ready flash.
        if(updatesExpanded_&&controller_&&controller_->updates().status().isEmpty())controller_->updates().load();
        render();
    });
    QObject::connect(signOut_,&QPushButton::clicked,this,[this]{
        if(!controller_||!controller_->canSignOut())return;
        updatesExpanded_=false;updateSection_->hide();updatesButton_->setGlyph(ButtonGlyph::Down);
        confirmingSignOut_=true;confirmationContext_=controller_->status().loginContext;render();
    });
    QObject::connect(signOutCancel_,&QPushButton::clicked,this,[this]{confirmingSignOut_=false;confirmationContext_.clear();render();});
    QObject::connect(signOutConfirm_,&QPushButton::clicked,this,[this]{
        if(!confirmingSignOut_||!controller_||!controller_->canSignOut()||controller_->status().loginContext!=confirmationContext_)return;
        confirmingSignOut_=false;confirmationContext_.clear();controller_->act(Action::SignOut);render();
    });
    QObject::connect(automaticChecks_,&QPushButton::clicked,this,[this]{if(controller_)controller_->updates().setAutomatic(!controller_->updates().status().value("automatic").toBool(true));});
    QObject::connect(checkUpdates_,&QPushButton::clicked,this,[this]{if(controller_&&!controller_->busy())controller_->updates().check();});
    QObject::connect(installTailscale_,&QPushButton::clicked,this,[this]{if(controller_)controller_->act(Action::InstallTailscale);});
    QObject::connect(installAddon_,&QPushButton::clicked,this,[this]{if(controller_)controller_->act(Action::InstallAddon);});
    QObject::connect(restart_,&QPushButton::clicked,this,[this]{if(controller_)controller_->act(Action::Restart);});
    QObject::connect(primary_,&QPushButton::clicked,this,[this]{
        if(!controller_)return;
        if(!controller_->runtimeReady()){controller_->prepareRuntime();return;}
        const Status status=controller_->status();
        controller_->act(controller_->connecting()?Action::Cancel:status.state==State::Connected?Action::Disconnect:status.requiresSignIn?Action::SignIn:Action::Connect);
    });
    QObject::connect(controller,&QObject::destroyed,this,[this]{hide();});
    QObject::connect(anchor,&QObject::destroyed,this,[this]{hide();});
    controller_->observe(this,[this]{render();});
    placement_.setSingleShot(true);QObject::connect(&placement_,&QTimer::timeout,this,[this]{place();});
    qApp->installEventFilter(this);render();
}
Panel::~Panel() { qApp->removeEventFilter(this); }
void Panel::open() {
    if(!controller_||!anchor_||!anchor_->isVisible()||anchor_->window()!=parentWidget())return;
    confirmingSignOut_=false;confirmationContext_.clear();
    updatesExpanded_=false;updateSection_->hide();updatesButton_->setGlyph(ButtonGlyph::Down);
    scroll_->verticalScrollBar()->setValue(0);render();place();
    if(geometry().isEmpty())return;
    raise();show();
}
void Panel::toggle() { if(isVisible())hide();else open(); }
void Panel::showEvent(QShowEvent *e) { QWidget::showEvent(e);if(controller_)controller_->setPanelVisible(this,true);schedulePlacement(); }
void Panel::hideEvent(QHideEvent *e) { confirmingSignOut_=false;confirmationContext_.clear();placement_.stop();dragging_=false;if(controller_)controller_->setPanelVisible(this,false);QWidget::hideEvent(e); }
void Panel::schedulePlacement() { if(isVisible()&&!placement_.isActive())placement_.start(0); }
void Panel::place() {
    QWidget *window=parentWidget();
    if(!window||!anchor_||!anchor_->isVisible()||anchor_->window()!=window){hide();return;}
    const QRect anchorRect(anchor_->mapTo(window,QPoint()),anchor_->size());
    const int width=qMin(760,window->width()-2*unit_),padding=unit_;
    int inner=qMax(1,width-2*padding);
    renderLogin(loginUrl_,qrPixels(inner));
    // The section owns a separate layout: activate it before measuring the
    // outer column so its cached height includes the newly populated labels.
    if(updatesExpanded_)updateSection_->layout()->activate();
    column_->activate();
    int contentHeight=column_->totalHeightForWidth(inner);
    if(contentHeight<0)contentHeight=column_->sizeHint().height();
    PopupPlacement p=placePopup(window->rect(),anchorRect,QSize(width,contentHeight+2*padding+arrow_),unit_,arrow_);
    if(p.frame.isEmpty()){hide();setGeometry(QRect());return;}
    const QRect viewport(padding,arrow_+padding,p.frame.width()-2*padding,p.frame.height()-arrow_-2*padding);
    if(contentHeight>viewport.height())inner-=scroll_->style()->pixelMetric(QStyle::PM_ScrollBarExtent);
    contentHeight=qMax(viewport.height(),column_->totalHeightForWidth(qMax(1,inner)));
    if(geometry()!=p.frame)setGeometry(p.frame);
    if(tipX_!=p.tipX){tipX_=p.tipX;update();}
    if(scroll_->geometry()!=viewport)scroll_->setGeometry(viewport);
    const QSize body(qMax(1,inner),contentHeight);if(content_->size()!=body)content_->resize(body);
    column_->activate();
}
void Panel::paintEvent(QPaintEvent *) {
    QPainter p(this);p.setPen(QPen(Qt::black,2));p.setBrush(Qt::white);
    QPolygon outline;outline<<QPoint(1,arrow_)<<QPoint(tipX_-arrow_,arrow_)<<QPoint(tipX_,1)<<QPoint(tipX_+arrow_,arrow_)<<QPoint(width()-2,arrow_)<<QPoint(width()-2,height()-2)<<QPoint(1,height()-2);
    p.drawPolygon(outline);
}
bool Panel::eventFilter(QObject *object,QEvent *e) {
    QWidget *widget=qobject_cast<QWidget*>(object),*window=parentWidget();
    if(object==anchor_&&(e->type()==QEvent::Hide||e->type()==QEvent::ParentChange))hide();
    if(object==window&&(e->type()==QEvent::Hide||e->type()==QEvent::WindowDeactivate))hide();
    if(isVisible()&&widget&&widget!=window&&widget->isWindow()&&e->type()==QEvent::Show&&
        (widget->windowType()==Qt::Popup||widget->windowType()==Qt::Dialog||widget->windowType()==Qt::Tool))hide();
    const bool anchors=anchor_&&widget&&(widget==anchor_||widget->isAncestorOf(anchor_));
    if(anchors&&e->type()==QEvent::ParentChange)hide();
    if(isVisible()&&anchors&&(e->type()==QEvent::Move||e->type()==QEvent::Resize))schedulePlacement();
    if(isVisible()&&e->type()==QEvent::LayoutRequest&&(object==content_||object==updateSection_))schedulePlacement();
    // Native touch reaches QWidgetWindow before QWidget touch acceptance is
    // resolved. Books/reading views need not accept QTouchEvent themselves.
    const bool nativeWindow=window&&object==window->windowHandle();
    if(!window||(!widget&&!nativeWindow)||(widget&&widget->window()!=window))return false;
    const bool mouse=e->type()==QEvent::MouseButtonPress||e->type()==QEvent::MouseButtonRelease||e->type()==QEvent::MouseMove;
    if(mouse){
        if(dismissedTouch_.isValid()&&dismissedTouch_.elapsed()<600){e->accept();return true;}
        if(swallowMouse_){if(e->type()==QEvent::MouseButtonRelease)swallowMouse_=false;e->accept();return true;}
    }
    const bool touch=e->type()==QEvent::TouchBegin||e->type()==QEvent::TouchUpdate||e->type()==QEvent::TouchEnd||e->type()==QEvent::TouchCancel;
    if(touch&&swallowTouch_){
        if(e->type()==QEvent::TouchEnd||e->type()==QEvent::TouchCancel){swallowTouch_=false;dismissedTouch_.start();}
        e->accept();return true;
    }
    if(!isVisible())return false;
    if(e->type()==QEvent::MouseButtonPress||e->type()==QEvent::TouchBegin){
        QPoint global;
        if(mouse)global=static_cast<QMouseEvent*>(e)->globalPos();
        else {const auto points=static_cast<QTouchEvent*>(e)->touchPoints();if(points.isEmpty())return false;global=points.first().screenPos().toPoint();}
        if(!rect().contains(mapFromGlobal(global))&&!anchor_->rect().contains(anchor_->mapFromGlobal(global))){
            // Let another toolbar control receive the same tap after dismissal.
            QWidget *row=anchor_->parentWidget();const bool toolbar=row&&row!=window&&row->rect().contains(row->mapFromGlobal(global));
            hide();if(toolbar)return false;
            if(mouse)swallowMouse_=true;else swallowTouch_=true;e->accept();return true;
        }
    }
    if(nativeWindow)return false;
    if(touch&&(widget==scroll_->viewport()||widget==content_||content_->isAncestorOf(widget))&&!qobject_cast<QScrollBar*>(widget)){
        const auto points=static_cast<QTouchEvent*>(e)->touchPoints();
        if(e->type()==QEvent::TouchBegin&&!points.isEmpty()){
            dragging_=true;dragMoved_=false;buttonGesture_=qobject_cast<QPushButton*>(widget)!=nullptr;
            dragStart_=points.first().screenPos();dragScroll_=scroll_->verticalScrollBar()->value();
        }
        if(dragging_){
            if(!points.isEmpty()){
                const QPointF movement=points.first().screenPos()-dragStart_;
                dragMoved_=dragMoved_||movement.manhattanLength()>QApplication::startDragDistance();
                if(dragMoved_)scroll_->verticalScrollBar()->setValue(dragScroll_-movement.y());
            }
            const bool button=buttonGesture_;
            if(e->type()==QEvent::TouchEnd||e->type()==QEvent::TouchCancel){dragging_=false;dismissedTouch_.start();}
            // Buttons still own their tap/pressed state; a dragged tap is cancelled
            // by TouchButton while the same gesture scrolls this viewport.
            if(button)return false;
            e->accept();return true;
        }
    }
    return false;
}

int Panel::qrPixels(int bodyWidth) const {
    // Reserve room for a possible vertical scrollbar before encoding. Using
    // the same bound for render/place also avoids alternating cached widths.
    return qMax(0,qMin(420,bodyWidth-scroll_->style()->pixelMetric(QStyle::PM_ScrollBarExtent)));
}

void Panel::renderLogin(const QString &url,int maxPixels){
    const bool link=!url.isEmpty(),code=loginQr_->setUrl(url,maxPixels);
    text(loginCaption_,!link?QString():code?"Scan with your phone to sign in":"QR code unavailable. Use the link below.");
    loginCaption_->setVisible(link);loginQr_->setVisible(code);
    text(login_,!link?QString():(code?"Or open on your phone or computer:\n":"Open on your phone or computer:\n")+url);
    login_->setVisible(link);
}

void Panel::render() {
    if(!controller_)return;
    const Status status=controller_->status(); text(state_,controller_->preparing()?"Preparing Tailscale":stateLabel(status.state));
    QString identity=status.name; if(status.state==State::Connected&&!status.address.isEmpty())identity+=(identity.isEmpty()?"":"\n")+status.address;text(identity_,identity);identity_->setVisible(!identity.isEmpty());
    QString message=controller_->notice();
    if(message.isEmpty())message=!status.error.isEmpty()?status.error:status.health.join("\n");
    if(status.state==State::NoWifi&&message.isEmpty())message="Connect Wi-Fi using the Kobo's Wi-Fi control.";
    text(notice_,message);notice_->setVisible(!message.isEmpty());
    const bool canLogin=status.state!=State::Connected&&status.state!=State::Disconnected&&status.state!=State::Stopped;
    loginUrl_=canLogin?status.authUrl:QString();
    renderLogin(loginUrl_,qrPixels(qMin(760,parentWidget()->width()-2*unit_)-2*unit_));
    const bool busy=controller_->busy(),connecting=controller_->connecting();
    if(confirmingSignOut_&&(!status.hasLogin||status.loginContext!=confirmationContext_||busy||controller_->updates().busy())){
        confirmingSignOut_=false;confirmationContext_.clear();
    }
    signOut_->setVisible(status.hasLogin&&!confirmingSignOut_);
    signOut_->setEnabled(controller_->canSignOut());
    signOutSection_->setVisible(confirmingSignOut_);
    signOutConfirm_->setEnabled(confirmingSignOut_&&controller_->canSignOut());
    const QString primary=controller_->preparing()?"Preparing…":!controller_->runtimeReady()?"Retry setup":connecting?"Cancel":status.state==State::Connected?"Disconnect":status.requiresSignIn?"Sign in":"Connect";
    if(primary_->text()!=primary)primary_->setText(primary);
    primary_->setGlyph(controller_->preparing()?ButtonGlyph::Preparing:!controller_->runtimeReady()?ButtonGlyph::Refresh:connecting?ButtonGlyph::Close:status.state==State::Connected?ButtonGlyph::Disconnect:ButtonGlyph::Connect);
    primary_->setEnabled(!controller_->preparing()&&(controller_->runtimeReady()?(connecting||(!busy&&status.state!=State::NoWifi&&(!status.requiresSignIn||controller_->hasWifi()))):!busy));
    updatesButton_->setEnabled(controller_->runtimeReady()&&!controller_->preparing());
    if(updatesExpanded_){
        Updates &u=controller_->updates();const QJsonObject v=u.status();
        const QString current=v.value("current").toString("Unknown"),latest=v.value("latest").toString();
        text(updateVersions_,"Tailscale — upstream stable\nInstalled: "+current+"\nLatest: "+(latest.isEmpty()?"Check for updates":latest));
        text(addonVersions_,"k-tailscale addon\nInstalled: "+v.value("addon_current").toString("Unknown")+"\nLatest: "+(v.value("addon_latest").toString().isEmpty()?"Check for updates":v.value("addon_latest").toString()));
        QStringList messages;messages<<"Updates install only when you tap Install.";
        if(u.busy())messages<<"Checking update information…";
        if(busy)messages<<(v.value("progress").toString().isEmpty()?"An action is in progress. Keep the Kobo powered on.":v.value("progress").toString());
        if(v.value("checked").toDouble()>0)messages<<"Last checked: "+QDateTime::fromTime_t(static_cast<uint>(v.value("checked").toDouble())).toString("yyyy-MM-dd HH:mm");
        automaticChecks_->setText(v.value("automatic").toBool(true)?"Auto-check: On":"Auto-check: Off");
        automaticChecks_->setEnabled(!busy&&!u.busy()&&!v.isEmpty());
        if(v.value("pending").toBool())messages<<"Tailscale will finish validation when its daemon next starts.";
        if(v.value("restart").toBool())messages<<"Addon installed. Restart the Kobo to use it.";
        for(const QString &key:QStringList{"error","addon_error","reason"})if(!v.value(key).toString().isEmpty())messages<<v.value(key).toString();
        if(!u.message().isEmpty())messages<<u.message();
        if(!controller_->notice().isEmpty())messages<<controller_->notice();
        text(updateNotice_,messages.join("\n"));
        checkUpdates_->setEnabled(!busy&&!u.busy()&&controller_->hasWifi());
        const bool install=controller_->hasWifi()&&!busy&&!u.busy()&&v.value("can_install").toBool()&&!v.value("restart").toBool();
        installTailscale_->setVisible(v.value("available").toBool()&&!v.value("pending").toBool());
        installAddon_->setVisible(v.value("addon_available").toBool());
        installTailscale_->setEnabled(install&&v.value("available").toBool()&&!v.value("pending").toBool());
        installAddon_->setEnabled(install&&v.value("addon_available").toBool());
        restart_->setVisible(v.value("restart").toBool());restart_->setEnabled(!busy&&!u.busy());
    }
    if(isVisible())place();
}

} // namespace tailui
