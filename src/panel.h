#pragma once

#include "controller.h"
#include <QElapsedTimer>
#include <QTimer>
#include <QWidget>

class QLabel;
class QScrollArea;
class QVBoxLayout;

namespace tailui {
class TouchButton;
class QrWidget;
class Panel : public QWidget {
public:
    Panel(QWidget *window, Controller *controller, QWidget *anchor);
    ~Panel() override;
    void open();
    void toggle();
protected:
    void showEvent(QShowEvent *event) override;
    void hideEvent(QHideEvent *event) override;
    bool eventFilter(QObject *object, QEvent *event) override;
    void paintEvent(QPaintEvent *event) override;
private:
    void render();
    void renderLogin(const QString &url,int maxPixels);
    int qrPixels(int bodyWidth) const;
    void schedulePlacement();
    void place();
    QPointer<Controller> controller_;
    QPointer<QWidget> anchor_;
    QScrollArea *scroll_;
    QWidget *content_, *updateSection_, *signOutSection_;
    QVBoxLayout *column_;
    QLabel *state_, *identity_, *notice_, *loginCaption_, *login_, *updateVersions_, *addonVersions_, *updateNotice_;
    TouchButton *primary_, *updatesButton_, *checkUpdates_, *automaticChecks_, *installTailscale_, *installAddon_, *restart_;
    TouchButton *signOut_, *signOutCancel_, *signOutConfirm_;
    QrWidget *loginQr_;
    QString loginUrl_, confirmationContext_;
    QTimer placement_;
    QElapsedTimer dismissedTouch_;
    int unit_, arrow_, tipX_=0, dragScroll_=0;
    QPointF dragStart_;
    bool buttonGesture_=false,dragMoved_=false;
    bool confirmingSignOut_=false, updatesExpanded_=false, swallowMouse_=false, swallowTouch_=false, dragging_=false;
};
} // namespace tailui
