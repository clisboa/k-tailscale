// Adapted from Relio, Apache-2.0. See NOTICE.
#pragma once
#include "button_icons.h"
#include <QEvent>
#include <QElapsedTimer>
#include <QApplication>
#include <QTouchEvent>
#include <QPushButton>
#include <QStyleOptionButton>
#include <QStylePainter>

namespace tailui {
class TouchButton : public QPushButton {
public:
    TouchButton(const QString &text, QWidget *parent) : QPushButton(text, parent) {
        setAttribute(Qt::WA_AcceptTouchEvents); setFocusPolicy(Qt::NoFocus);
    }
    void setGlyph(ButtonGlyph glyph) {
        if(hasGlyph_&&glyph_==glyph)return;
        glyph_=glyph;hasGlyph_=true;
        setIcon(buttonIcon(glyph,iconSize().height()));
        pressedIcon_=buttonIcon(glyph,iconSize().height(),true);
    }
    ButtonGlyph glyph() const { return glyph_; }
protected:
    void paintEvent(QPaintEvent *event) override {
        if(!hasGlyph_){QPushButton::paintEvent(event);return;}
        QStylePainter painter(this);QStyleOptionButton option;initStyleOption(&option);
        if(isEnabled()&&isDown())option.icon=pressedIcon_;
        painter.drawControl(QStyle::CE_PushButton,option);
    }
    bool event(QEvent *event) override {
        switch (event->type()) {
        case QEvent::TouchBegin: {
            const auto points=static_cast<QTouchEvent*>(event)->touchPoints();
            touching_=isEnabled()&&!points.isEmpty()&&rect().contains(points.first().pos().toPoint());moved_=false;
            if(touching_)touchStart_=points.first().screenPos();
            setDown(touching_);event->accept();return true;
        }
        case QEvent::TouchUpdate: {
            const auto points=static_cast<QTouchEvent*>(event)->touchPoints();
            if(touching_&&!points.isEmpty()){
                moved_=moved_||(points.first().screenPos()-touchStart_).manhattanLength()>QApplication::startDragDistance();
                setDown(!moved_&&isEnabled()&&rect().contains(points.first().pos().toPoint()));
            }
            event->accept();return true;
        }
        case QEvent::TouchEnd: {
            const auto points=static_cast<QTouchEvent*>(event)->touchPoints();
            const bool tap=touching_&&!moved_&&isEnabled()&&!points.isEmpty()&&
                (points.first().screenPos()-touchStart_).manhattanLength()<=QApplication::startDragDistance()&&rect().contains(points.first().pos().toPoint());
            touching_=false;lastTouch_.start();setDown(false);event->accept();
            if(tap)click();
            return true;
        }
        case QEvent::TouchCancel:
            touching_=false;lastTouch_.start();setDown(false);event->accept();return true;
        case QEvent::Hide:
        case QEvent::EnabledChange:
            if(event->type()==QEvent::Hide||!isEnabled()){touching_=false;setDown(false);}
            return QPushButton::event(event);
        default:
            if((event->type()==QEvent::MouseButtonPress||event->type()==QEvent::MouseButtonRelease||event->type()==QEvent::MouseButtonDblClick)&&lastTouch_.isValid()&&lastTouch_.elapsed()<600){event->accept();return true;}
            return QPushButton::event(event);
        }
    }
private:
    QElapsedTimer lastTouch_;
    QPointF touchStart_;
    bool touching_=false,moved_=false;
    bool hasGlyph_=false;
    ButtonGlyph glyph_=ButtonGlyph::Connect;
    QIcon pressedIcon_;
};
} // namespace tailui
