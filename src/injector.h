// Toolbar discovery adapted from Relio, Apache-2.0. See NOTICE.
#pragma once
#include "controller.h"
#include <QObject>
#include <QPointer>
class QWidget;
namespace tailui {
class Injector : public QObject {
public:
    explicit Injector(Controller *controller,QObject *parent=nullptr) : QObject(parent),controller_(controller) {}
    void scanExisting();
protected:
    bool eventFilter(QObject *watched,QEvent *event) override;
private:
    void inject(QWidget *bar);
    QPointer<Controller> controller_;
};
} // namespace tailui
